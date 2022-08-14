package trp

type FrameType int

const (
	DATA FrameType = iota
	CLOSE
	BIND
)

type Frame struct {
	Id   string
	Type FrameType
	Data []byte
}

func (f *Frame) ParsePort() int {
	return ReadIntFrom2Bytes(f.Data)
}

func Assemble(frame *Frame) []byte {
	switch frame.Type {
	case DATA:
		totalLen := 32 + len(frame.Data)
		newBytes := make([]byte, 32)
		copy(newBytes, frame.Id)
		newBytes[16] = 0x0
		WriteIntTo8Bytes(newBytes[17:25], totalLen)
		return newBytes
	case CLOSE:
		newBytes := make([]byte, 32)
		copy(newBytes, frame.Id)
		newBytes[16] = 0x1
		return newBytes
	case BIND:
		newBytes := make([]byte, 32)
		copy(newBytes, frame.Id)
		newBytes[16] = 0x2
		copy(newBytes[17:19], frame.Data[:2])
		return newBytes
	}
	return nil
}

func ParseAll(node *SliceLink[byte]) ([]Frame, []byte) {
	ret := make([]Frame, 0)
	remain := Parse(node, &ret)
	return ret, remain.Data()
}

func Parse(buf *SliceLink[byte], frames *[]Frame) *SliceLink[byte] {
	if buf.Len() < 32 {
		return buf
	}
	id := string(buf.SubToEnd(16).Data())
	switch buf.Get(16) {
	case 0x0:
		totalLen := ReadIntFrom8Bytes(buf.Sub(17, 25).Data())
		if totalLen > buf.Len() {
			return buf
		}
		*frames = append(*frames, Frame{
			Id:   id,
			Type: DATA,
			Data: buf.Sub(32, totalLen).Data(),
		})
		return Parse(buf.SubFromStart(totalLen), frames)
	case 0x1:
		*frames = append(*frames, Frame{
			Id:   id,
			Type: CLOSE,
		})
		return Parse(buf.SubFromStart(32), frames)
	case 0x2:
		*frames = append(*frames, Frame{
			Id:   id,
			Type: BIND,
			Data: buf.Sub(17, 19).Data(),
		})
		return Parse(buf.SubFromStart(32), frames)
	}
	return nil
}
