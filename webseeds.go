package libtorrent

import (
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/anacrolix/missinggo/bitmap"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

const WEBSEED_URL_CONCURENT = 2 // how many concurent downloading per one Url
const WEBSEED_CONCURENT = 2     // how many concurent downloading total

// http://bittorrent.org/beps/bep_0019.html

var webseedstorage map[metainfo.Hash]*webSeeds

// sine we can dynamically add / done webSeeds, we have add one per call
func webSeedOpen(t *torrent.Torrent) {
	hash := t.InfoHash()
	var ws *webSeeds
	if w, ok := webseedstorage[hash]; ok { // currenlty active webseeds for torrent
		ws = w
	} else {
		ws = &webSeeds{}
	}
	webseedstorage[hash] = ws

	if len(ws.ww) >= WEBSEED_CONCURENT { // limit? exit
		return
	}

	if ws.uu == nil {
		uu := t.UrlList()
		if len(uu) == 0 { // no webseed urls? exit
			return
		}
		for _, u := range uu {
			e := &webUrl{url: u}
			e.Extract()
			ws.uu = append(ws.uu, e)
		}
		sort.Sort(ByRange(ws.uu)) // sort source urls by 'Range' and maybe speed
	}

	torrentstorageLock.Lock() // ts block
	ts := torrentstorage[hash]

	info := ts.info
	checks := ts.checks

	var ff []*webFile // get files to download (unfinished files from torrent)

	var offset int64
	for i, fi := range info.UpvertedFiles() {
		s := offset / info.PieceLength
		e := (offset + fi.Length) / info.PieceLength
		r := (offset + fi.Length) % info.PieceLength
		if r > 0 {
			e++
		}
		if checks[i] {
			completed := &bitmap.Bitmap{}
			bm := &bitmap.Bitmap{}
			bm.AddRange(int(s), int(e))
			done := true // all pieces belong to file should be complete
			min := math.MaxInt32
			max := -1
			bm.IterTyped(func(piece int) (again bool) {
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
				return true // continue
			})
			if !done {
				bm.Sub(*completed)
				path := strings.Join(append([]string{ts.info.Name}, fi.Path...), "/")            // keep original torrent name unrenamed
				ff = append(ff, &webFile{path, offset, fi.Length, int(s), int(e), bm, min, max}) // [s, e)
			}
		}
		offset += fi.Length
	}

	torrentstorageLock.Unlock() // ts block

	// find not downloading files first and add them to webSeed, then return
	for _, f := range ff {
		downloading := false // check if file currenlty downloading by one of webSeed
		for _, w := range ws.ww {
			if w.file.path == f.path {
				downloading = true
				break
			}
		}
		if !downloading {
			for _, u := range ws.uu { // choise right url, skip url if it is limited
				if u == u { // TODO find right url
					w := &webSeed{t, u, f, f.start, f.end}
					ws.ww = append(ws.ww, w)
					webSeedOpen(t)
					return
				}
			}
			return // exit if no url found
		}
	}

	// all files downloading in the array, find first and split it
	for _, f := range ff {
		for _, w := range ws.ww {
			if w.file.path == f.path {
				// split file by two
				for _, u := range ws.uu { // choise right url, skip url if it is limited
					if u == u { // TODO find right url
						w := &webSeed{t, u, f, f.start, f.end}
						ws.ww = append(ws.ww, w)
						webSeedOpen(t)
						return
					}
				}
			}
		}
	}
}

func webSeedClose(t *torrent.Torrent) {
	hash := t.InfoHash()
	ww := webseedstorage[hash]
	for _, v := range ww.ww {
		v.Close()
	}
	delete(webseedstorage, hash)
}

type webSeeds struct {
	uu []*webUrl  // source url extraceted and cleared if url broken / slow / has missing files
	ww []*webSeed // current seeds
}

type webFile struct {
	path   string         // file name
	offset int64          // torrent offset
	length int64          // file length
	start  int            // [start piece
	end    int            // end) piece
	bm     *bitmap.Bitmap // pieces to download
	bmmin  int            // [start piece min
	bmmax  int            // end] piece max
}

type ByRange []*webUrl

func (m ByRange) Len() int           { return len(m) }
func (m ByRange) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }
func (m ByRange) Less(i, j int) bool { return m[i].r && !m[j].r }

var CONTENT_RANGE = regexp.MustCompile("bytes (\\d+)-(\\d+)/(\\d+)")

// web url, keep url information (resume support? mulitple connections?)
type webUrl struct {
	url    string // source url
	r      bool   // http RANGE support?
	length int64  // file url size (content-size)
	speed  int    // download bytes per seconds, helps choice best source
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
	t     *torrent.Torrent
	url   *webUrl  // url to download from
	file  *webFile // current file
	start int      // start piece number (can be bigger then file.start)
	end   int      // end piece number (can be lower then file.end)
}

func (m *webSeed) Start() {
	go m.Run()
}

func (m *webSeed) Run() {
	defer func() {
		m.Close()
		webSeedOpen(m.t)
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

	pstart := int64(min) * pieceLength           // piece offset bytes start
	pend := int64(max)*pieceLength + pieceLength // piece offset bytes end
	if pstart < m.file.offset {
		pstart = m.file.offset
	}
	if pend > fend {
		pend = fend
	}

	rstart := pstart - fstart // RANGE offset start
	rend := pend - fstart     // RANGE offset end

	offset := m.file.offset + rstart // torrent offset in bytes

	if strings.HasPrefix(m.url.url, "http") {
		req, err := http.NewRequest("GET", m.url.url, nil)
		if err != nil {
			return
		}
		// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Content-Range
		//
		// Range: <unit>=<range-start>-
		// Range: <unit>=<range-start>-<range-end>
		// Range: <unit>=<range-start>-<range-end>, <range-start>-<range-end>
		// Range: <unit>=<range-start>-<range-end>, <range-start>-<range-end>, <range-start>-<range-end>
		//
		// "Range: bytes=200-1000, 2000-6576, 19000-"
		req.Header.Add("Range", "bytes="+strconv.FormatInt(rstart, 10)+"-"+strconv.FormatInt(rend, 10))
		var client http.Client
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		buf := make([]byte, 4*1024)
		for n, _ := resp.Body.Read(buf); n != 0; offset += int64(n) {
			m.t.WriteChunk(offset, buf[:n])
		}
	}
}

func (m *webSeed) Close() {
	// TODO close http call
}
