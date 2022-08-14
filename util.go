package trp

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"strings"
)

func init() {
	flag.IntVar(&logLevel, "d", 1, "debug")
}

func WriteIntTo8Bytes(bytes []byte, v int) {
	binary.LittleEndian.PutUint64(bytes, uint64(v))
}

func ReadIntFrom8Bytes(bytes []byte) int {
	return int(binary.LittleEndian.Uint64(bytes[0:8]))
}

func IntTo2Bytes(v int) []byte {
	b := make([]byte, 2)
	binary.LittleEndian.PutUint16(b, uint16(v))
	return b
}

func ReadIntFrom2Bytes(bytes []byte) int {
	return int(binary.LittleEndian.Uint16(bytes[0:2]))
}

func LogBytes(logger *log.Logger, bytes []byte) {
	switch logLevel {
	case 1:
		_ = logger.Output(2, fmt.Sprintf("Data size: %d", len(bytes)))
	case 2:
		for _, s := range strings.Split(string(bytes), "\n") {
			_ = logger.Output(2, s)
		}
	default:

	}
}
