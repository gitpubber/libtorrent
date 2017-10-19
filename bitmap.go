package libtorrent

import (
	"github.com/anacrolix/missinggo/bitmap"
)

func bitmapIntersects(bm *bitmap.Bitmap, s int, e int) bool {
	fb := &bitmap.Bitmap{}
	fb.AddRange(int(s), int(e))
	return bitmapIntersectsBm(bm, fb)
}

func bitmapIntersectsBm(bm *bitmap.Bitmap, w *bitmap.Bitmap) bool { // original roaring.Intersects hidden by bitmap.Bitmap
	n := bm.Copy()
	old := n.Len()
	n.Sub(*w)
	return n.Len() != old
}

func bitmapAnd(bm *bitmap.Bitmap, selected *bitmap.Bitmap) *bitmap.Bitmap {
	and := &bitmap.Bitmap{} // add file with range of piecec selected
	bm.IterTyped(func(piece int) (again bool) {
		if selected.Contains(piece) {
			and.Add(piece)
		}
		return true
	})
	return and
}
