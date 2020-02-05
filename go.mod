module gitlab.com/axet/libtorrent

go 1.13

require (
	github.com/anacrolix/missinggo v1.2.1
	github.com/anacrolix/torrent v1.13.0
	github.com/syncthing/syncthing v1.3.4
	golang.org/x/time v0.0.0-20191024005414-555d28b269f0
)

replace github.com/anacrolix/torrent v1.13.0 => gitlab.com/axet/torrent v0.0.0-20200205141541-92b4b9e7387e
