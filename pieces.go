package libtorrent

const (
	PieceEmpty    int32 = 0 // gray
	PieceComplete int32 = 1 // blue
	PieceChecking int32 = 2 // yellow
	PiecePartial  int32 = 3 // green, when booth empty and completed
	PieceWriting  int32 = 4 // red, when have partial pieces
	PieceUnpended int32 = 5 // ltgray, empy pieces can be unpended
)

func TorrentPieceLength(i int) int64 {
	mu.Lock()
	defer mu.Unlock()
	t := torrents[i]
	return t.Info().PieceLength
}

func TorrentPiecesCount(i int) int {
	mu.Lock()
	defer mu.Unlock()
	t := torrents[i]
	return t.NumPieces()
}

func TorrentPiecesCompactCount(i int, size int) int {
	mu.Lock()
	defer mu.Unlock()

	t := torrents[i]
	fs := filestorage[t.InfoHash()]
	fs.Pieces = nil

	pended := false
	empty := false
	complete := false
	partial := false
	checking := false
	count := 0

	pack := func() {
		state := PieceEmpty
		if checking {
			state = PieceChecking
		} else if partial {
			state = PieceWriting
		} else if empty && complete {
			state = PiecePartial
		} else if complete {
			state = PieceComplete
		} else if !pended {
			state = PieceUnpended
		} else {
			state = PieceEmpty
		}
		fs.Pieces = append(fs.Pieces, state)
	}

	pos := 0
	for _, v := range t.PieceStateRuns() {
		for i := 0; i < v.Length; i++ {
			if v.Complete {
				complete = true
			} else {
				empty = true
				if t.PiecePended(pos) { // at least one pice pendend then mark all (size) pended
					pended = true
				}
			}
			if v.Partial {
				partial = true
			}
			if v.Checking {
				checking = true
			}
			count = count + 1

			if count >= size {
				pack()
				pended = false
				empty = false
				complete = false
				partial = false
				checking = false
				count = 0
			}
			pos++
		}
	}
	if count > 0 {
		pack()
	}
	return len(fs.Pieces)
}

func TorrentPiecesCompact(i int, p int) int32 {
	mu.Lock()
	defer mu.Unlock()
	t := torrents[i]
	f := filestorage[t.InfoHash()]
	return f.Pieces[p]
}
