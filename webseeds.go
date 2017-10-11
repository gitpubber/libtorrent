package libtorrent

import (
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/anacrolix/missinggo/bitmap"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

// http://bittorrent.org/beps/bep_0019.html

var webseedstorage map[metainfo.Hash]webSeeds

func webSeedStart(t *torrent.Torrent) {
	hash := t.InfoHash()
	var ws *webSeeds
	if ws, ok := webseedstorage[hash]; !ok { // currenlty active webseeds for torrent
		ws = webSeeds{}
	}

	torrentstorageLock.Lock()
	defer torrentstorageLock.Unlock()
	ts := torrentstorage[hash]

	info := ts.info
	checks := ts.checks

	PieceLength := info.PieceLength

	if ws.uu == nil {
		uu := t.UrlList() // source urls
		for _, u := range uu {
			e := &webUrl{url: u}
			e.Extract()
			ws.uu = append(ws.uu, e)
		}
		sort.Sort(ByRange(ws.uu)) // sort source urls by 'Range' and maybe speed
	}

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
			bm.IterTyped(func(piece int) (again bool) {
				if t.PieceBytesCompleted(piece) == t.PieceLength(piece) {
					completed.Add(piece)
					return true // continue
				} else {
					done = false // one of the pieece not complete
					return false // stop looping
				}
			})
			if !done {
				bm.Sub(*completed)
				ff = append(ff, &webFile{fi.Path, offset, fi.Length, s, e, bm}) // [s, e)
			}
		}
		offset += fi.Length
	}

	switch len(ff) {
	case 0: // break
	case 1: // if here is only one unfinished file left and all source urls norange, use only one source
		norange := true
		for _, u := range ws.uu {
			if u.Range {
				norange = false
			}
		}
		if norange { // use only one webSeed
			ws.add()
		} else { // use multiple []webSeed
			ws.adds()
		}
	default: // here multiple files, use multi sources
		ws.adds()
	}

	webseedstorage[hash] = ws
}

func webSeedStop(t *torrent.Torrent) {
	hash := t.InfoHash()
	ww := webseedstorage[hash]
	for _, v := range ww {
		v.Close()
	}
	delete(webseedstorage, hash)
}

type webSeeds struct {
	uu []*webUrl  // source url extraceted and cleared if url broken / has missing files
	ww []*webSeed // current seeds
}

func (m *webSeeds) Add() {
	w := &webSeed{url, t, "", 0, 1}
	go w.Run()
	ww = append(ww, w)
	w = &webSeed{url, t, "", 2, 3}
	go w.Run()
	ws.ww = append(ws.ww, w)
}

func (m *webSeeds) Adds() {
	for _, w := range ww { // no range. check if we have current file oppened, skip to next
		w = w
	}
	w := &webSeed{url, t, "", -1, -1}
	go w.Run()
	ws.ww = append(ws.ww, w)
}

type webFile struct {
	file   string         // file name
	offset int64          // torrent offset
	length int64          // file length
	start  int            // [start piece
	end    int            // end) piece
	bm     *bitmap.Bitmap // pieces to download
}

type ByRange []*webUrl

func (m ByRange) Len() int           { return len(m) }
func (m ByRange) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }
func (m ByRange) Less(i, j int) bool { return m[i].Range && !m[j].Range }

var CONTENT_RANGE = regexp.MustCompile("bytes (\\d+)-(\\d+)/(\\d+)")

// web url, keep url information (resume support? mulitple connections?)
type webUrl struct {
	url    string // source url
	r      bool   // http RANGE support?
	length int64  // file url size (content-size)
	speed  int    // download bytes per seconds, helps choice best source
}

func (m *webUrl) Extract() {
	if strings.HasPrefix(m.Url, "http") {
		req, err := http.NewRequest("GET", m.Url, nil)
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
		g := p.FindStringSubmatch(CONTENT_RANGE)
		if len(g) > 0 {
			m.Length, err = strconv.ParseInt(g[3], 10, 64)
		}
		m.Range = true
	}
}

type webSeed struct {
	url   *webUrl // url to download from
	t     *torrent.Torrent
	file  string // current file name
	start int    // start piece number
	end   int    // end piece number
}

func (m *webSeed) Run() {
	defer m.Close()
	info := m.T.Info()
	var r io.Reader
	if strings.HasPrefix(m.Url.Url, "http") {
		req, err := http.NewRequest("GET", m.Url.Url, nil)
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
		req.Header.Add("Range", "bytes="+strconv.Itoa(min)+"-"+strconv.Itoa(max-1))
		var client http.Client
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		r = resp.Body
		// reader, _ := ioutil.ReadAll(resp.Body)
	}
	buf := make([]byte, 1024)
	n, err := r.Read(buf)
	if err != nil {
		return
	}
	for _, fi := range info.UpvertedFiles() {
		n = n
		fi = fi
	}
}

func (m *webSeed) Close() {
}
