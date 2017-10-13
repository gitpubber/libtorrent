package libtorrent

import (
	"context"
	"log"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/anacrolix/missinggo/bitmap"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

// http://bittorrent.org/beps/bep_0017.html - httpseeds
// http://bittorrent.org/beps/bep_0019.html - url-list

const WEBSEED_URL_CONCURENT = 2        // how many concurent downloading per one Url
const WEBSEED_CONCURENT = 4            // how many concurent downloading total
const WEBSEED_SPLIT = 10 * 1024 * 1024 // how large split for single sizes
const WEBSEED_BUF = 4 * 1024           // read buffer size

var webseedstorage map[metainfo.Hash]*webSeeds

func TorrentWebSeedsCount(i int) int {
	mu.Lock()
	defer mu.Unlock()

	t := torrents[i]
	fs := filestorage[t.InfoHash()]

	return len(fs.UrlList)
}

func TorrentWebSeeds(i int, p int) string {
	mu.Lock()
	defer mu.Unlock()

	t := torrents[i]
	fs := filestorage[t.InfoHash()]

	return fs.UrlList[p]
}

// sine we can dynamically add / done webSeeds, we have add one per call
func webSeedStart(t *torrent.Torrent) {
	hash := t.InfoHash()
	var ws *webSeeds
	if w, ok := webseedstorage[hash]; ok { // currenlty active webseeds for torrent
		ws = w
	} else {
		ws = &webSeeds{}
		info := t.Info()
		ws.t = t
		ws.pieces = make([][]int64, info.NumPieces())
		chunks := info.PieceLength / int64(t.GetChunkSize())
		log.Println("chunks", info.NumPieces(), chunks)
		for i := range ws.pieces {
			ws.pieces[i] = make([]int64, chunks)
		}
		ws.ww = make(map[*webSeed]bool)
	}
	webseedstorage[hash] = ws

	if len(ws.ww) >= WEBSEED_CONCURENT { // limit? exit
		return
	}

	fs := filestorage[t.InfoHash()]

	if ws.uu == nil {
		ws.uu = make(map[*webUrl]bool)
		uu := fs.UrlList
		log.Println(t.Info().Name, len(uu), uu)
		if len(uu) == 0 { // no webseed urls? exit
			return
		}
		for _, u := range uu {
			e := &webUrl{url: u}
			e.Extract()
			log.Println("extracted", e.url)
			ws.uu[e] = true
		}
	}

	torrentstorageLock.Lock() // ts block
	ts := torrentstorage[hash]

	info := ts.info
	checks := ts.checks

	pieceLength := info.PieceLength

	if ws.ff == nil {
		ws.ff = make(map[*webFile]bool)
		var offset int64
		for i, fi := range info.UpvertedFiles() {
			s := offset / info.PieceLength
			e := (offset + fi.Length) / info.PieceLength
			r := (offset + fi.Length) % info.PieceLength
			if r > 0 {
				e++
			}
			if checks[i] {
				bm := &bitmap.Bitmap{}
				bm.AddRange(int(s), int(e))
				path := strings.Join(append([]string{ts.info.Name}, fi.Path...), "/") // keep original torrent name unrenamed
				f := &webFile{path, offset, fi.Length, int(s), int(e), bm, -1, -1, 0} // [s, e)
				ws.ff[f] = true
			}
			offset += fi.Length
		}
	}

	if len(ws.ff) == 0 {
		log.Println("all files done")
		return
	}

	for f := range ws.ff {
		completed := &bitmap.Bitmap{}
		done := true         // all pieces belong to file should be complete
		min := math.MaxInt32 // first piece to download
		max := -1            // last piece to download
		if f.downloaded < f.length {
			f.bm.IterTyped(func(piece int) (again bool) {
				if t.PieceBytesCompleted(piece) == t.PieceLength(piece) {
					completed.Add(piece)
				} else {
					done = false // one of the piece not complete
					if piece < min {
						min = piece
					}
					if piece > max {
						max = piece
					}
				}
				return true
			})
		}
		if !done {
			f.bm.Sub(*completed)
			f.bmmin = min
			f.bmmax = max
		} else {
			log.Println("file done", f.path)
			delete(ws.ff, f)
		}
	}

	torrentstorageLock.Unlock() // ts block

	// find not downloading files first and add them to webSeed, then return
	for f := range ws.ff {
		downloading := false // check if file currenlty downloading by one of webSeed
		for w := range ws.ww {
			if w.file.path == f.path {
				downloading = true
				break
			}
		}
		if !downloading {
			for u := range ws.uu { // choise right url, skip url if it is limited
				count := ws.UrlUseCount(u) // how many concurent downloads per url
				if count < WEBSEED_URL_CONCURENT {
					w := &webSeed{ws, t, u, f, f.start, f.end, nil, nil}
					ws.ww[w] = true
					w.Start()
					log.Println("add webseed", f.path, w.start, w.end)
					webSeedStart(t)
					return
				}
			}
			return // exit if no url found, all url limited or broken
		}
	}

	return

	// all files downloading in the array, find first and split it
	for w := range ws.ww {
		l := w.file.bmmax + 1 - w.file.bmmin // pieces length
		ld := int64(l) / 2 * pieceLength     // download size divide by 2
		if ld > WEBSEED_SPLIT {
			for u := range ws.uu { // choise right url, skip url if it is limited
				if u.r {
					count := ws.UrlUseCount(u) // how many concurent downloads per url
					if count < WEBSEED_URL_CONCURENT {
						parts := WEBSEED_CONCURENT
						piecesCount := l / parts // pieces count
						for int64(piecesCount)*pieceLength < WEBSEED_SPLIT {
							parts--
							piecesCount = l / parts
						}
						w.end = w.file.bmmin + piecesCount
						w2 := &webSeed{ws, t, u, w.file, w.end, w.end + piecesCount, nil, nil}
						log.Println("split webseed", w.file.path, w.start, w.end)
						ws.ww[w2] = true
						w2.Start()
						webSeedStart(t)
						return
					}
				}
			}
		}
	}
}

func webSeedStop(t *torrent.Torrent) {
	hash := t.InfoHash()
	ww := webseedstorage[hash]
	for v := range ww.ww {
		v.Close()
	}
	delete(webseedstorage, hash)
}

type webSeeds struct {
	t      *torrent.Torrent
	pieces [][]int64         // pieces / chunk size map
	uu     map[*webUrl]bool  // source url extraceted and cleared if url broken / slow / has missing files
	ff     map[*webFile]bool // files to download, cleard for completed files
	ww     map[*webSeed]bool // current downloading seeds
}

func (m *webSeeds) UrlUseCount(u *webUrl) int {
	count := 0
	for w := range m.ww {
		if w.url == u {
			count = count + 1
		}
	}
	return count
}

func (m *webSeeds) ChunkSize(pieceIndex int, chunkIndex int) int64 {
	ChunkSize := int64(m.t.GetChunkSize())
	return ChunkSize // TODO last chunk in pieace / torrent can be less then ChunkSize
}

func (m *webSeeds) WriteChunk(offset int64, buf []byte) {
	info := m.t.Info()

	pieceIndex := int(offset / info.PieceLength)

	ps := offset / info.PieceLength // start piece

	chunkOffset := offset - ps*info.PieceLength

	bufLen := int64(len(buf))
	ChunkSize := int64(m.t.GetChunkSize())

	chunkStart := int(chunkOffset / ChunkSize)
	chunkEnd := int((chunkOffset + bufLen) / ChunkSize)
	chunkFull := int64(0)
	for i := chunkStart; i < chunkEnd; i++ {
		m.pieces[pieceIndex][i] += ChunkSize
		chunkFull += ChunkSize
	}
	if chunkEnd < len(m.pieces[pieceIndex]) {
		m.pieces[pieceIndex][chunkEnd] += bufLen - chunkFull
	}

	for i := range m.pieces[pieceIndex] {
		if m.pieces[pieceIndex][i] >= m.ChunkSize(pieceIndex, i) {
			m.t.UnpendChunk(pieceIndex, i) // unpend only fully downloaded chunks
		}
	}

	m.t.WriteChunk(offset, buf)
}

type webFile struct {
	path       string         // file name
	offset     int64          // torrent offset
	length     int64          // file length
	start      int            // [start piece
	end        int            // end) piece
	bm         *bitmap.Bitmap // pieces to download
	bmmin      int            // [start piece min
	bmmax      int            // end] piece max
	downloaded int64          // total bytes downloaded
}

var CONTENT_RANGE = regexp.MustCompile("bytes (\\d+)-(\\d+)/(\\d+)")

// web url, keep url information (resume support? mulitple connections?)
type webUrl struct {
	url    string // source url
	r      bool   // http RANGE support?
	length int64  // file url size (content-size)
	count  int    // how many requests (load balancing)
}

func (m *webUrl) Extract() {
	if strings.HasPrefix(m.url, "http") {
		req, err := http.NewRequest("GET", m.url, nil)
		if err != nil {
			return
		}
		req.Header.Add("Range", "bytes=0-0")
		var client http.Client
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		r := resp.Header.Get("Content-Range")
		if r == "" {
			return
		}
		// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Content-Range
		//
		// Content-Range: <unit> <range-start>-<range-end>/<size>
		// Content-Range: <unit> <range-start>-<range-end>/*
		// Content-Range: <unit> */<size>
		//
		// Content-Range: bytes 200-1000/67589
		g := CONTENT_RANGE.FindStringSubmatch(r)
		if len(g) > 0 {
			m.length, err = strconv.ParseInt(g[3], 10, 64)
		}
		m.r = true
	}
}

type webSeed struct {
	ws    *webSeeds
	t     *torrent.Torrent
	url   *webUrl  // url to download from
	file  *webFile // current file
	start int      // start piece number (can be bigger then file.start)
	end   int      // end piece number (can be lower then file.end)

	req    *http.Request
	cancel context.CancelFunc
}

func (m *webSeed) Start() {
	if !strings.HasPrefix(m.url.url, "http") {
		return
	}

	req, err := http.NewRequest("GET", m.url.url+"/"+m.file.path, nil)
	if err != nil {
		return
	}

	cx, cancel := context.WithCancel(context.Background())
	m.req = req.WithContext(cx)
	m.cancel = cancel
	go m.Run()
}

func (m *webSeed) Run() {
	next := false
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		m.autoClose()
		if next {
			webSeedStart(m.t)
		}
	}()

	info := m.t.Info()

	pieceLength := info.PieceLength

	min := m.start   // [start url piece
	max := m.end - 1 // end] url piece
	if min < m.file.bmmin {
		min = m.file.bmmin // file min piece bigger then webSeed one
	}
	if max > m.file.bmmax {
		max = m.file.bmmax // file max piece lower then webSeed one
	}

	fstart := m.file.offset        // file bytes start
	fend := fstart + m.file.length // file bytes end

	pstart := int64(min) * pieceLength // piece offset bytes start
	pend := int64(max+1) * pieceLength // piece offset bytes end
	if pstart < m.file.offset {
		pstart = m.file.offset
	}
	if pend > fend {
		pend = fend
	}

	rstart := pstart - fstart // RANGE offset start
	rend := pend - fstart     // RANGE offset end

	if m.url.r {
		// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Content-Range
		//
		// Range: <unit>=<range-start>-
		// Range: <unit>=<range-start>-<range-end>
		// Range: <unit>=<range-start>-<range-end>, <range-start>-<range-end>
		// Range: <unit>=<range-start>-<range-end>, <range-start>-<range-end>, <range-start>-<range-end>
		//
		// "Range: bytes=200-1000, 2000-6576, 19000-"
		m.req.Header.Add("Range", "bytes="+strconv.FormatInt(rstart, 10)+"-"+strconv.FormatInt(rend, 10))
	} else {
		rstart = 0
		rend = pend
	}

	offset := m.file.offset + rstart // torrent offset in bytes

	var client http.Client
	resp, err := client.Do(m.req)
	if err != nil {
		next = true
		return // TODO remove 404 urls
	}
	defer resp.Body.Close()

	buf := make([]byte, WEBSEED_BUF)
	for true {
		if m.req == nil { // canceled
			return // return, no next
		}
		n, err := resp.Body.Read(buf)
		if n == 0 && err != nil { // done
			m.file.downloaded = m.file.downloaded + (offset - rstart)
			next = true
			return // start next webSeed
		}
		m.ws.WriteChunk(offset, buf[:n])

		offset += int64(n)

		mu.Lock()
		k := int64(m.end) * pieceLength // update pend
		mu.Unlock()

		if k < pend {
			pend = k // new pend less then old one?
		}
		if offset > pend { // reached end of webSeed.end (overriden by new webSeed)
			m.file.downloaded = m.file.downloaded + (pend - rstart)
			next = true
			return // start next webSeed
		}
	}
}

func (m *webSeed) autoClose() {
	delete(m.ws.ww, m)
}

func (m *webSeed) Close() {
	if m.req != nil {
		m.cancel()
		m.req = nil
	}
	m.autoClose()
}
