package libtorrent

import (
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/anacrolix/missinggo"
	"github.com/anacrolix/missinggo/bitmap"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

var filestorage map[metainfo.Hash]*fileStorage
var storageExternal FileStorageTorrent
var torrentstorage map[metainfo.Hash]*torrentStorage
var torrentstorageLock sync.Mutex

type FileStorageTorrent interface {
	ReadFileAt(hash string, path string, buf *Buffer, off int64) (n int, err error) // java unable to change []byte buf if it passed as a parameter
	WriteFileAt(hash string, path string, b []byte, off int64) (n int, err error)
	Remove(hash string, path string) error
	Rename(hash string, old string, path string) error
}

func TorrentStorageSet(p FileStorageTorrent) {
	torrentstorageLock.Lock()
	defer torrentstorageLock.Unlock()
	storageExternal = p
}

type Buffer struct {
	buf []byte
}

func (m *Buffer) Write(b []byte, pos int, len int) (int, error) {
	return copy(m.buf, b[pos:pos+len]), nil
}

func (m *Buffer) Length() int {
	return len(m.buf)
}

type fileStorage struct {
	// dynamic data
	Trackers []Tracker
	Pieces   []int32
	Files    []File
	Peers    []Peer

	// date in seconds when torrent been StartTorrent, we measure this value to get downloadingTime && seedingTime
	ActivateDate int64

	// elapsed in seconds
	DownloadingTime int64
	SeedingTime     int64

	// dates in seconds
	AddedDate     int64
	CompletedDate int64

	// .torrent info
	Creator   string
	CreatedOn int64
	Comment   string

	UrlList []WebSeedUrl
}

func registerFileStorage(info metainfo.Hash, path string) *fileStorage {
	ts := &torrentStorage{path: path}

	torrentstorageLock.Lock()
	torrentstorage[info] = ts
	torrentstorageLock.Unlock()

	fs := &fileStorage{
		AddedDate: time.Now().UnixNano(),
		Comment:   "dynamic metainfo from client",
		Creator:   "go.libtorrent",
		CreatedOn: time.Now().UnixNano(),
	}

	filestorage[info] = fs

	return fs
}

type torrentStorage struct {
	info            *metainfo.Info
	infoHash        metainfo.Hash
	path            string
	checks          []bool
	completedPieces bitmap.Bitmap
	root            string // new torrent name if renamed

	completed bool // fired when torrent downloaded, used for queue engine to roll downloads
	next      missinggo.Event
}

func (m *torrentStorage) Checks() []bool {
	// lock outside
	checks := make([]bool, len(m.checks))
	copy(checks, m.checks)
	return checks
}

func (m *torrentStorage) Pieces() []bool {
	// lock outside
	bf := make([]bool, m.info.NumPieces())
	m.completedPieces.IterTyped(func(piece int) (again bool) {
		bf[piece] = true
		return true
	})
	return bf
}

func (m *torrentStorage) Completed() {
	// lock outside
	fb := filePendingBitmapTs(m.info, m.checks)

	m.completed = true

	fb.IterTyped(func(piece int) (again bool) { // run thougth all pieces and check they all present in m.completedPieces
		if !m.completedPieces.Contains(piece) {
			m.completed = false
			return false
		}
		return true
	})
}

type torrentOpener struct {
}

func (m *torrentOpener) Close() error {
	return nil
}

func (m *torrentOpener) OpenTorrent(info *metainfo.Info, infoHash metainfo.Hash) (storage.TorrentImpl, error) {
	torrentstorageLock.Lock()
	defer torrentstorageLock.Unlock()

	ts := torrentstorage[infoHash]
	ts.info = info
	ts.infoHash = infoHash

	if ts.checks == nil { // if we come here from LoadTorrent checks is set. otherwise we come here after torrent open, fill defaults
		ts.checks = make([]bool, len(info.UpvertedFiles()))
		for i, _ := range ts.checks {
			ts.checks[i] = true
		}
	}

	ts.Completed() // update comleted after torrent open

	return &fileTorrentStorage{ts}, nil
}

type fileTorrentStorage struct {
	ts *torrentStorage
}

func (m *fileTorrentStorage) Piece(p metainfo.Piece) storage.PieceImpl {
	// Create a view onto the file-based torrent storage.
	_io := &fileStorageTorrent{
		p.Info,
		m.ts,
		m.ts.infoHash.HexString(),
	}
	// Return the appropriate segments of this.
	return &fileStoragePiece{
		m.ts,
		p,
		missinggo.NewSectionWriter(_io, p.Offset(), p.Length()),
		io.NewSectionReader(_io, p.Offset(), p.Length()),
	}
}

func (fs *fileTorrentStorage) Close() error {
	return nil
}

type fileStoragePiece struct {
	*torrentStorage
	p metainfo.Piece
	io.WriterAt
	io.ReaderAt
}

func (m *fileStoragePiece) Completion() storage.Completion {
	torrentstorageLock.Lock()
	defer torrentstorageLock.Unlock()
	return storage.Completion{Complete: m.completedPieces.Get(m.p.Index()), Ok: true}
}

func (m *fileStoragePiece) MarkComplete() error {
	torrentstorageLock.Lock()
	defer torrentstorageLock.Unlock()
	m.completedPieces.Set(m.p.Index(), true)

	if m.completed {
		return nil
	}

	if storageExternal != nil { // create zero flies only once, after torrent downloaded
		for _, fi := range m.info.UpvertedFiles() {
			if fi.Length != 0 {
				continue
			}
			var path []string
			if len(fi.PathUTF8) != 0 {
				path = fi.PathUTF8
			} else {
				path = fi.Path
			}
			name := filepath.Join(append([]string{m.info.Name}, path...)...)
			_, err := storageExternal.WriteFileAt(m.infoHash.HexString(), name, []byte{}, 0)
			if err != nil {
				return err
			}
		}
	} else {
		err := storage.CreateNativeZeroLengthFiles(m.info, m.path)
		if err != nil {
			return err
		}
	}

	m.Completed()

	if m.completed {
		m.next.Set()
	}

	return nil
}

func (m *fileStoragePiece) MarkNotComplete() error {
	torrentstorageLock.Lock()
	defer torrentstorageLock.Unlock()
	m.completedPieces.Set(m.p.Index(), false)
	return nil
}

type fileStorageTorrent struct {
	info *metainfo.Info
	ts   *torrentStorage
	hash string // hash string for external calls
}

// Returns EOF on short or missing file.
func (fst *fileStorageTorrent) readFileAt(fi metainfo.FileInfo, b []byte, off int64) (n int, err error) {
	torrentstorageLock.Lock()
	rel := fst.fileRel(fi)
	path := fst.fileRoot(rel)
	s := storageExternal
	torrentstorageLock.Unlock()
	if s != nil {
		return storageExternal.ReadFileAt(fst.hash, rel, &Buffer{b}, off)
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		err = io.EOF // File missing is treated the same as a short file.
		return
	}
	if err != nil {
		return
	}
	defer f.Close()
	if int64(len(b)) > fi.Length-off { // Limit the read to within the expected bounds of this file.
		b = b[:fi.Length-off]
	}
	for off < fi.Length && len(b) != 0 {
		n1, err1 := f.ReadAt(b, off)
		b = b[n1:]
		n += n1
		off += int64(n1)
		if n1 == 0 {
			err = err1
			break
		}
	}
	return
}

// Only returns EOF at the end of the torrent. Premature EOF is ErrUnexpectedEOF.
func (fst *fileStorageTorrent) ReadAt(b []byte, off int64) (n int, err error) {
	for _, fi := range fst.info.UpvertedFiles() {
		for off < fi.Length {
			n1, err1 := fst.readFileAt(fi, b, off)
			n += n1
			off += int64(n1)
			b = b[n1:]
			if len(b) == 0 {
				// Got what we need.
				return
			}
			if n1 != 0 {
				// Made progress.
				continue
			}
			err = err1
			if err == io.EOF {
				// Lies.
				err = io.ErrUnexpectedEOF
			}
			return
		}
		off -= fi.Length
	}
	err = io.EOF
	return
}

func (fst *fileStorageTorrent) WriteAt(p []byte, off int64) (n int, err error) {
	for _, fi := range fst.info.UpvertedFiles() {
		if off >= fi.Length {
			off -= fi.Length
			continue
		}
		n1 := len(p)
		if int64(n1) > fi.Length-off {
			n1 = int(fi.Length - off)
		}
		torrentstorageLock.Lock()
		rel := fst.fileRel(fi)
		path := fst.fileRoot(rel)
		s := storageExternal
		torrentstorageLock.Unlock()
		if s != nil {
			n1, err = s.WriteFileAt(fst.hash, rel, p[:n1], off)
			if err != nil {
				return
			}
		} else {
			os.MkdirAll(filepath.Dir(path), 0770)
			var f *os.File
			f, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0660)
			if err != nil {
				return
			}
			n1, err = f.WriteAt(p[:n1], off)
			f.Close()
			if err != nil {
				return
			}
		}
		n += n1
		off = 0 // next file offset
		p = p[n1:]
		if len(p) == 0 {
			break
		}
	}
	return
}

func (fst *fileStorageTorrent) fileRel(fi metainfo.FileInfo) string {
	name := fst.ts.root
	if name == "" { // torrent hasen't been renamed
		name = fst.info.Name // use original name
	}
	var path []string
	if len(fi.PathUTF8) != 0 {
		path = fi.PathUTF8
	} else {
		path = fi.Path
	}
	return filepath.Join(append([]string{name}, path...)...)
}

func (fst *fileStorageTorrent) fileRoot(rel string) string {
	return filepath.Join(append([]string{fst.ts.path}, rel)...)
}
