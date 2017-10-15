package libtorrent

import (
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/missinggo/bitmap"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

// http://bittorrent.org/beps/bep_0017.html - httpseeds
// http://bittorrent.org/beps/bep_0019.html - url-list

const WEBSEED_URL_CONCURENT = 2                        // how many concurent downloading per one Url
const WEBSEED_CONCURENT = 4                            // how many concurent downloading total
const WEBSEED_SPLIT = 10 * 1024 * 1024                 // how large split for single sizes
const WEBSEED_BUF = 64 * 1024                          // read buffer size
const WEBSEED_TIMEOUT = time.Duration(5 * time.Second) // dial up and socket read timeouts

var webseedstorage map[metainfo.Hash]*webSeeds

type WebSeedUrl struct {
	Url        string
	Downloaded int64  // total bytes / speed test
	Error      string // error if url were removed
}

func TorrentWebSeedsCount(i int) int {
	mu.Lock()
	defer mu.Unlock()

	t := torrents[i]
	hash := t.InfoHash()
	fs := filestorage[hash]

	return len(fs.UrlList)
}

func TorrentWebSeeds(i int, p int) *WebSeedUrl {
	mu.Lock()
	defer mu.Unlock()

	t := torrents[i]
	hash := t.InfoHash()
	fs := filestorage[hash]

	return &fs.UrlList[p]
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
		ws.chunks = make([][]int64, info.NumPieces())
		ws.ww = make(map[*webSeed]bool)
	}
	webseedstorage[hash] = ws

	if len(ws.ww) >= WEBSEED_CONCURENT { // limit? exit
		return
	}

	fs := filestorage[t.InfoHash()]

	if ws.uu == nil {
		ws.uu = make(map[*webUrl]bool)
		if len(fs.UrlList) == 0 { // no webseed urls? exit
			return
		}
		for i := range fs.UrlList {
			u := &fs.UrlList[i]
			u.Error = "" // clear error on restarts
			e := &webUrl{url: u.Url, ws: u}
			ws.uu[e] = true
		}
		mu.Unlock()
		for u := range ws.uu {
			err := u.Extract()
			if err != nil {
				u.ws.Error = err.Error()
			}
		}
		mu.Lock()
	}

	torrentstorageLock.Lock() // ts block
	ts := torrentstorage[hash]

	info := ts.info
	checks := ts.checks

	pieceLength := info.PieceLength

	if ws.ff == nil {
		ws.ff = make(map[*webFile]bool)
		selected := &bitmap.Bitmap{}
		{ // add user selected files
			var offset int64
			for i, fi := range info.UpvertedFiles() {
				s := offset / info.PieceLength
				e := (offset + fi.Length) / info.PieceLength
				r := (offset + fi.Length) % info.PieceLength
				if r > 0 {
					e++
				}
				if checks[i] {
					selected.AddRange(int(s), int(e))
					bm := &bitmap.Bitmap{}
					bm.AddRange(int(s), int(e))
					path := strings.Join(append([]string{ts.info.Name}, fi.Path...), "/") // keep original torrent name unrenamed
					f := &webFile{path, offset, fi.Length, int(s), int(e), bm, -1, -1, 0} // [s, e)
					ws.ff[f] = true
				}
				offset += fi.Length
			}
		}
		{ // add rest pices files
			var offset int64
			for _, fi := range info.UpvertedFiles() {
				s := offset / info.PieceLength
				e := (offset + fi.Length) / info.PieceLength
				r := (offset + fi.Length) % info.PieceLength
				if r > 0 {
					e++
				}

				path := strings.Join(append([]string{ts.info.Name}, fi.Path...), "/") // keep original torrent name unrenamed

				found := false
				for f := range ws.ff {
					if f.path == path {
						found = true
					}
				}

				if !found { // if file is not selected
					bm := &bitmap.Bitmap{}
					bm.AddRange(int(s), int(e))
					if bitmapIntersectsBm(selected, bm) { // and it belong to picece selected
						and := bitmapAnd(bm, selected)
						f := &webFile{path, offset, fi.Length, int(s), int(e), and, -1, -1, 0}
						ws.ff[f] = true
					}
				}

				offset += fi.Length
			}
		}
	}

	if len(ws.ff) == 0 {
		return
	}

	for f := range ws.ff {
		completed := &bitmap.Bitmap{}
		done := true         // all pieces belong to file should be complete
		min := math.MaxInt32 // first piece to download
		max := -1            // last piece to download
		if f.downloaded < f.length {
			f.bm.IterTyped(func(piece int) (again bool) {
				if ts.completedPieces.Get(piece) {
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
			for w := range ws.ww {
				if w.file == f {
					w.Close()
				}
			}
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
				if u.e {
					count := ws.UrlUseCount(u) // how many concurent downloads per url
					if count < WEBSEED_URL_CONCURENT {
						w := &webSeed{ws, t, u, f, f.start, f.end, nil}
						ws.ww[w] = true
						w.Start()
						webSeedStart(t)
						return
					}
				}
			}
			return // exit if no url found, all url limited or broken
		}
	}

	// all files downloading in the array, find first and split it
	for w1 := range ws.ww {
		for u := range ws.uu { // choise right url, skip url if it is limited
			if u.e && u.r {
				count := ws.UrlUseCount(u) // how many concurent downloads per url
				if count < WEBSEED_URL_CONCURENT {
					fileParts := w1.file.bmmax - w1.file.bmmin + 1 // how many undownloaded pieces in a file
					splitCount := WEBSEED_CONCURENT
					piecesGrab := fileParts / splitCount // how many pieces to grab per webSeed
					for int64(piecesGrab)*pieceLength < WEBSEED_SPLIT && splitCount > 1 {
						splitCount-- // webSeed smaller then WEBSEED_SPLIT, increase side by reducing splits
						piecesGrab = fileParts / splitCount
					}
					if splitCount > 1 { // abble to split?
						w1l := w1.end - w1.start // w pices to download
						if w1l > piecesGrab {
							end := w1.end
							w1.end = w1.start + piecesGrab
							w2 := &webSeed{ws, t, u, w1.file, w1.end, end, nil}
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

	for u := range ws.uu { // check if we have not extracted url (timeout on first call)
		if !u.e {
			u.ws.Error = ""
			err := u.Extract()
			if err != nil {
				u.ws.Error = err.Error()
			}
			if u.e {
				webSeedStart(t)
			}
			return // extracte one by one
		}
	}
}

func webSeedStop(t *torrent.Torrent) {
	hash := t.InfoHash()
	if ws, ok := webseedstorage[hash]; ok {
		for v := range ws.ww {
			v.Close()
		}
		delete(webseedstorage, hash)
	}
}

func deleteUrl(resp *http.Response) bool {
	if resp != nil {
		switch resp.StatusCode {
		case 403, 404:
			return true
		}
	}
	return false
}

func dialTimeout(req *http.Request) (*http.Response, net.Conn, error) {
	var conn net.Conn
	transport := http.Transport{
		Dial: func(netw, addr string) (c net.Conn, err error) {
			conn, err = net.DialTimeout(netw, addr, WEBSEED_TIMEOUT)
			return conn, err
		},
	}
	client := http.Client{
		Transport: &transport,
	}
	resp, err := client.Do(req)
	return resp, conn, err
}

func formatWebSeed(w *webSeed) string {
	str := ""
	str += fmt.Sprintf("[%d,%d] ", w.start, w.end)
	return str
}

func formatWebSeeds(ws *webSeeds) string {
	str := ""
	for w := range ws.ww {
		str += formatWebSeed(w)
	}
	return str
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
	e      bool   // extracted?
	r      bool   // http RANGE support?
	length int64  // file url size (content-size)
	ws     *WebSeedUrl
}

func (m *webUrl) Extract() error {
	if strings.HasPrefix(m.url, "http") {
		req, err := http.NewRequest("GET", m.url, nil)
		if err != nil {
			return err
		}
		req.Header.Add("Range", "bytes=0-0")
		resp, _, err := dialTimeout(req)
		if err != nil {
			return err
		}
		m.e = true
		r := resp.Header.Get("Content-Range")
		if r == "" {
			return nil
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
		m.r = true // RANGE supported
	}
	return nil
}

type webSeed struct {
	ws    *webSeeds
	t     *torrent.Torrent
	url   *webUrl  // url to download from
	file  *webFile // current file
	start int      // start piece number (can be bigger then file.start)
	end   int      // end piece number (can be lower then file.end)

	cancel context.CancelFunc
}

func (m *webSeed) Start() {
	if !strings.HasPrefix(m.url.url, "http") {
		return
	}

	var url string

	if len(m.ws.ff) > 1 {
		url = m.url.url + "/" + m.file.path
	} else {
		url = m.url.url
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return
	}

	cx, cancel := context.WithCancel(context.Background())
	req = req.WithContext(cx)
	m.cancel = cancel
	go m.Run(req)
}

func (m *webSeed) Run(req *http.Request) {
	next := false
	var del error

	defer func() {
		mu.Lock()
		defer mu.Unlock()
		m.autoClose()
		if del != nil {
			m.ws.DeleteUrl(m.url, del)
		}
		if next {
			webSeedStart(m.t)
		}
	}()

	info := m.t.Info()

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

	pstart := int64(min) * info.PieceLength // piece offset bytes start
	pend := int64(max+1) * info.PieceLength // piece offset bytes end

	if pstart < m.file.offset {
		pstart = m.file.offset
	}
	if pend > fend {
		pend = fend
	}

	rmin := pstart - fstart // RANGE offset start
	rmax := pend - fstart   // RANGE offset end

	if m.url.r {
		// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Content-Range
		//
		// Range: <unit>=<range-start>-
		// Range: <unit>=<range-start>-<range-end>
		// Range: <unit>=<range-start>-<range-end>, <range-start>-<range-end>
		// Range: <unit>=<range-start>-<range-end>, <range-start>-<range-end>, <range-start>-<range-end>
		//
		// "Range: bytes=200-1000, 2000-6576, 19000-"
		req.Header.Add("Range", "bytes="+strconv.FormatInt(rmin, 10)+"-"+strconv.FormatInt(rmax, 10))
	} else {
		rmin = 0
		rmax = pend
	}

	offsetStart := m.file.offset + rmin // torrent offset in bytes
	offset := offsetStart

	resp, conn, err := dialTimeout(req)
	if err != nil {
		log.Println("download error", formatWebSeed(m), err)
		next = true
		if deleteUrl(resp) {
			del = err // delete source url
		}
		return // start next webSeed
	}
	defer resp.Body.Close()

	buf := make([]byte, WEBSEED_BUF)
	for true {
		if m.cancel == nil { // canceled
			return // return, no next
		}
		conn.SetDeadline(time.Now().Add(WEBSEED_TIMEOUT))
		n, err := resp.Body.Read(buf)
		if n == 0 { // done
			m.file.downloaded += offset - offsetStart
			next = true
			if err != io.EOF {
				log.Println("download error", formatWebSeed(m), err)
			}
			return // start next webSeed
		}
		m.t.WriteChunk(offset, buf[:n], m.ws.chunks)

		m.url.ws.Downloaded += int64(n)

		offset += int64(n)

		mu.Lock()
		k := int64(m.end) * info.PieceLength // update pend
		mu.Unlock()
		if k < pend {
			pend = k // new pend less then old one?
		}

		if offset > pend { // reached end of webSeed.end (overriden by new webSeed)
			m.file.downloaded += pend - offsetStart
			next = true
			return // start next webSeed
		}
	}
}

func (m *webSeed) autoClose() {
	delete(m.ws.ww, m)
}

func (m *webSeed) Close() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.autoClose()
}

type webSeeds struct {
	t      *torrent.Torrent
	chunks [][]int64         // pieces / chunk size map
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

func (m webSeeds) DeleteUrl(u *webUrl, err error) {
	delete(m.uu, u)
	u.ws.Error = err.Error()
}
