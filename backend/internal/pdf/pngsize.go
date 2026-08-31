package pdf

import (
	"encoding/binary"
	"fmt"
	"os"
)

func PNGSize(path string) (width, height int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	hdr := make([]byte, 24)
	if _, err := f.ReadAt(hdr, 0); err != nil {
		return 0, 0, err
	}
	if string(hdr[1:4]) != "PNG" {
		return 0, 0, fmt.Errorf("não é um PNG: %s", path)
	}
	width = int(binary.BigEndian.Uint32(hdr[16:20]))
	height = int(binary.BigEndian.Uint32(hdr[20:24]))
	return width, height, nil
}
