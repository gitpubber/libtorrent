package libtorrent

import (
	"context"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
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

func WebSeedStart(t *torrent.Torrent) {
	mu.Lock()
	defer mu.Unlock()

	if _, ok := active[t]; !ok {
		return // called on paused torrent
	}

	webSeedStart(t)
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
		webseedstorage[hash] = ws
	}

	if len(ws.ww) >= WEBSEED_CONCURENT { // limit? exit
		return
	}

	fs := filestorage[hash]

	if len(fs.UrlList) == 0 { // no webseed urls? exit
		return
	}

	torrentstorageLock.Lock() // ts block
	ts := torrentstorage[hash]

	info := ts.info

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
				if ts.checks[i] {
					selected.AddRange(int(s), int(e))
					bm := &bitmap.Bitmap{}
					bm.AddRange(int(s), int(e))
					path := strings.Join(append([]string{ts.info.Name}, fi.Path...), "/") // keep original torrent name unrenamed
					f := &webFile{path, offset, fi.Length, int(s), int(e), bm, 0}         // [s, e)
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
						break
					}
				}

				if !found { // if file is not selected
					bm := &bitmap.Bitmap{}
					bm.AddRange(int(s), int(e))
					if bitmapIntersectsBm(selected, bm) { // and it belong to picece selected
						and := bitmapAnd(bm, selected)
						f := &webFile{path, offset, fi.Length, int(s), int(e), and, 0}
						ws.ff[f] = true
					}
				}

				offset += fi.Length
			}
		}
	}

	for f := range ws.ff {
		completed := &bitmap.Bitmap{}
		done := true // all pieces belong to file should be complete
		if f.downloaded < f.length {
			f.bm.IterTyped(func(piece int) (again bool) {
				if ts.completedPieces.Get(piece) {
					completed.Add(piece)
				} else {
					done = false // one of the piece not complete
				}
				return true
			})
		}
		if !done {
			f.bm.Sub(*completed)
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

	if len(ws.ff) == 0 {
		return
	}

	if ws.uu == nil {
		ws.uu = make(map[*webUrl]bool)
		for i := range fs.UrlList {
			u := &fs.UrlList[i]
			u.Error = "" // clear error on restarts
			e := &webUrl{url: u.Url, wsu: u}
			ws.uu[e] = true
		}
		for u := range ws.uu {
			ws.Extract(u)
		}
	}

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
				if ws.UrlReady(u) {
					w := &webSeed{ws, t, u, f, f.start, f.end, nil}
					ws.ww[w] = true
					w.Start()
					webSeedStart(t)
					return
				}
			}
		}
	}

	// all files downloading in the array, find first and split it
	for w1 := range ws.ww {
		for u := range ws.uu { // choise right url, skip url if it is limited
			if ws.UrlReady(u) && u.r {
				fileParts := w1.file.bm.Len() // how many undownloaded pieces in a file
				splitCount := WEBSEED_CONCURENT
				piecesGrab := fileParts / splitCount // how many pieces to grab per webSeed
				for int64(piecesGrab)*info.PieceLength < WEBSEED_SPLIT && splitCount > 1 {
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

	now := time.Now().UnixNano()
	for u := range ws.uu { // check if we have not extracted url (timeout on first call)
		if !u.e || u.n > now {
			u.wsu.Error = ""
			u.n = 0
			u.e = false
			ws.Extract(u)
			if u.e {
				webSeedStart(t)
			}
			if len(ws.ww) == 0 { // check if all urls are broken and not downloading
				all := true
				for k := range ws.uu {
					if k.e && k.n == 0 {
						all = false
					}
				}
				if all {
					go func() {
						time.Sleep(WEBSEED_TIMEOUT)
						WebSeedStart(t) // then start delayed checks
					}()
				}
			}
			return // no next. extracte one by one
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
	downloaded int64          // total bytes downloaded
}

var CONTENT_RANGE = regexp.MustCompile("bytes (\\d+)-(\\d+)/(\\d+)")

// web url, keep url information (resume support? mulitple connections?)
type webUrl struct {
	url string      // source url
	e   bool        // extracted?
	r   bool        // http RANGE support?
	wsu *WebSeedUrl // user url object
	n   int64       // time, restore deleted url after
}

func (m *webUrl) Get(path string) (*http.Request, context.CancelFunc, error) {
	url := m.url
	if path != "" {
		if !strings.HasSuffix(url, "/") {
			url += "/"
		}
		url += path
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, nil, err
	}
	cx, cancel := context.WithCancel(context.Background())
	req = req.WithContext(cx)
	return req, cancel, nil
}

func (m *webUrl) Extract(path string) error {
	if strings.HasPrefix(m.url, "http") {
		req, _, err := m.Get(path)
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
			_, err = strconv.ParseInt(g[3], 10, 64)
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

	path := ""

	if len(m.ws.ff) > 1 { // multi file torrent, url points to set of files
		path = m.file.path
	}

	req, cancel, err := m.url.Get(path)
	if err != nil {
		return
	}
	m.cancel = cancel
	go m.Run(req)
}

func (m *webSeed) Run(req *http.Request) {
	next := false
	var del error

	defer func() {
		mu.Lock()
		m.autoClose()
		if del != nil {
			m.ws.UrlDelete(m.url, del)
		}
		mu.Unlock()
		if next {
			WebSeedStart(m.t)
		}
	}()

	info := m.t.Info()

	fstart := m.file.offset        // file bytes start
	fend := fstart + m.file.length // file bytes end

	end := int64(m.end) * info.PieceLength // bytes end by 'weebseed'
	if end > fend {
		end = fend // bytes end by 'file'
	}

	var parts [][]int64 // [part][rmin, rmax, size]
	if m.url.r {
		// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Range
		//
		// Range: <unit>=<range-start>-
		// Range: <unit>=<range-start>-<range-end>
		// Range: <unit>=<range-start>-<range-end>, <range-start>-<range-end>
		// Range: <unit>=<range-start>-<range-end>, <range-start>-<range-end>, <range-start>-<range-end>
		//
		// "Range: bytes=200-1000, 2000-6576, 19000-"
		const COMSP = ", "
		start := -1
		count := 0
		bytes := "bytes="
		fadd := func(start, count int) {
			rmin := int64(start) * info.PieceLength
			if rmin < fstart {
				rmin = fstart
			}
			rmin = rmin - fstart
			rmax := int64(start+count)*info.PieceLength - 1
			if rmax > fend {
				rmax = fend
			}
			rmax = rmax - fstart
			parts = append(parts, []int64{rmin, rmax, 0})
			bytes += strconv.FormatInt(rmin, 10) + "-" + strconv.FormatInt(rmax, 10) + COMSP
		}
		m.file.bm.IterTyped(func(piece int) (again bool) {
			if piece >= m.start && piece <= m.end {
				if piece == start+count {
					count++
				} else {
					if start != -1 {
						fadd(start, count)
					}
					start = piece
					count = 1
				}
			}
			return true
		})
		if count > 0 {
			fadd(start, count)
		}
		bytes = strings.TrimSuffix(bytes, COMSP)
		req.Header.Add("Range", bytes)
	} else {
		rmin := int64(0)
		rmax := end - 1
		parts = append(parts, []int64{rmin, rmax, 0})
	}

	resp, conn, err := dialTimeout(req)

	mu.Lock()
	cancel := m.cancel
	mu.Unlock()
	if cancel == nil { // canceled
		return // return, no next
	}

	if err != nil {
		log.Println("download error", formatWebSeed(m), err)
		next = true
		if resp != nil {
			switch resp.StatusCode {
			case 403, 404:
				del = err // delete source url
			}
		}
		return // start next webSeed
	}
	defer resp.Body.Close()

	var r io.Reader
	ct := resp.Header["Content-Type"]
	if strings.HasPrefix(ct[0], "multipart/") {
		_, params, err := mime.ParseMediaType(ct[0])
		if err != nil {
			next = true
			log.Println("download error", formatWebSeed(m), err)
			del = err
			return // next, failed for multipart errors
		}
		mr := multipart.NewReader(resp.Body, params["boundary"])
		r = &MultipartReader{mr: mr}
	} else {
		r = &BodyReader{resp: resp}
	}

	i := 0
	buf := make([]byte, WEBSEED_BUF)
	for {
		conn.SetDeadline(time.Now().Add(WEBSEED_TIMEOUT))
		n, err := r.Read(buf)

		mu.Lock()
		k := int64(m.end) * info.PieceLength // update end
		cancel := m.cancel
		mu.Unlock()
		if k < end {
			end = k // new end less then old one
		}
		if cancel == nil { // canceled
			return // return, no next
		}

		if n == 0 { // done
			mu.Lock()
			for _, p := range parts {
				m.file.downloaded += p[2]
			}
			mu.Unlock()
			next = true
			if err != io.EOF {
				log.Println("download error", formatWebSeed(m), err)
			}
			return // start next webSeed
		}

		mu.Lock()
		m.url.wsu.Downloaded += int64(n) // speedtest
		mu.Unlock()

		rest := buf[:n]
		for n > 0 {
			p := parts[i]
			old := p[2]
			rmin := p[0]
			rmax := p[1]
			plen := rmax - rmin + 1
			pn := old + int64(n)
			if pn > plen {
				n = int(plen - old)
			}
			offset := fstart + rmin + old
			m.t.WriteChunk(offset, rest[:n], m.ws.chunks) // updated 'n'
			p[2] += int64(n)
			if p[2] >= plen {
				i++
			}

			pend := fstart + rmin + p[2]
			if pend > end { // reached end of webSeed.end (overriden by new webSeed)
				mu.Lock()
				size := int64(0)
				for _, p := range parts {
					pend := p[0] + p[2]
					if pend > end {
						s := end - pend
						if s > 0 { // should never be < 0
							size += s
						}
						break
					}
					size += p[2]
				}
				m.file.downloaded += size
				mu.Unlock()
				next = true
				return // start next webSeed
			}

			rest = rest[n:]
			n = len(rest)
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

func (m *webSeeds) UrlReady(u *webUrl) bool {
	if u.e && u.n == 0 {
		count := m.UrlUseCount(u) // how many concurent downloads per url
		if count < WEBSEED_URL_CONCURENT {
			return true
		}
	}
	return false
}

func (m *webSeeds) UrlDelete(u *webUrl, err error) {
	u.wsu.Error = err.Error()
	u.n = time.Now().Add(WEBSEED_TIMEOUT).UnixNano()
}

func (m *webSeeds) Extract(u *webUrl) error {
	path := ""
	if len(m.ff) > 1 {
		for f := range m.ff {
			path = f.path
			break
		}
	}
	var err error
	func() { // auto lock after panic()
		mu.Unlock()
		defer mu.Lock()
		err = u.Extract(path)
	}()
	if err != nil {
		m.UrlDelete(u, err)
	}
	return err
}

type BodyReader struct {
	resp *http.Response
}

func (m *BodyReader) Read(b []byte) (int, error) {
	return m.resp.Body.Read(b)
}

type MultipartReader struct {
	mr *multipart.Reader
	p  *multipart.Part
}

func (m *MultipartReader) Read(b []byte) (int, error) {
	if m.p == nil {
		m.p, err = m.mr.NextPart()
		if err != nil {
			return 0, err
		}
	}
	n, err := m.p.Read(b)
	if n == 0 {
		if err == io.EOF {
			m.p = nil
			return m.Read(b)
		}
	}
	return n, err
}
