package binary

import (
	"fmt"
	"math"
	"strconv"

	"go.mau.fi/whatsmeow/binary/token"
)

type binaryEncoder struct {
	data []byte
	md   bool
}

func NewEncoder(md bool) *binaryEncoder {
	data := make([]byte, 0)
	if md {
		data = []byte{0}
	}
	return &binaryEncoder{data, md}
}

func (w *binaryEncoder) GetData() []byte {
	return w.data
}

func (w *binaryEncoder) pushByte(b byte) {
	w.data = append(w.data, b)
}

func (w *binaryEncoder) pushBytes(bytes []byte) {
	w.data = append(w.data, bytes...)
}

func (w *binaryEncoder) pushIntN(value, n int, littleEndian bool) {
	for i := 0; i < n; i++ {
		var curShift int
		if littleEndian {
			curShift = i
		} else {
			curShift = n - i - 1
		}
		w.pushByte(byte((value >> uint(curShift*8)) & 0xFF))
	}
}

func (w *binaryEncoder) pushInt20(value int) {
	w.pushBytes([]byte{byte((value >> 16) & 0x0F), byte((value >> 8) & 0xFF), byte(value & 0xFF)})
}

func (w *binaryEncoder) pushInt8(value int) {
	w.pushIntN(value, 1, false)
}

func (w *binaryEncoder) pushInt16(value int) {
	w.pushIntN(value, 2, false)
}

func (w *binaryEncoder) pushInt32(value int) {
	w.pushIntN(value, 4, false)
}

func (w *binaryEncoder) pushInt64(value int) {
	w.pushIntN(value, 8, false)
}

func (w *binaryEncoder) pushString(value string) {
	w.pushBytes([]byte(value))
}

func (w *binaryEncoder) writeByteLength(length int) {
	if length < 256 {
		w.pushByte(token.Binary8)
		w.pushInt8(length)
	} else if length < (1 << 20) {
		w.pushByte(token.Binary20)
		w.pushInt20(length)
	} else if length < math.MaxUint32 {
		w.pushByte(token.Binary32)
		w.pushInt32(length)
	} else {
		panic(fmt.Errorf("length is too large: %d", length))
	}
}

const tagSize = 1

func (w *binaryEncoder) WriteNode(n Node) {
	if n.Tag == "0" {
		w.pushByte(token.List8)
		w.pushByte(token.ListEmpty)
		return
	}

	hasContent := 0
	if n.Content != nil {
		hasContent = 1
	}

	w.writeListStart(2*len(n.Attrs) + tagSize + hasContent)
	w.writeString(n.Tag)
	w.writeAttributes(n.Attrs)
	if n.Content != nil {
		w.write(n.Content)
	}
}

func (w *binaryEncoder) write(data interface{}) {
	switch typedData := data.(type) {
	case nil:
		w.pushByte(token.ListEmpty)
	case *FullJID:
		w.writeJID(*typedData)
	case FullJID:
		w.writeJID(typedData)
	case string:
		w.writeString(typedData)
	case int:
		w.writeString(strconv.Itoa(typedData))
	case int32:
		w.writeString(strconv.FormatInt(int64(typedData), 10))
	case uint:
		w.writeString(strconv.FormatUint(uint64(typedData), 10))
	case uint32:
		w.writeString(strconv.FormatUint(uint64(typedData), 10))
	case int64:
		w.writeString(strconv.FormatInt(typedData, 10))
	case uint64:
		w.writeString(strconv.FormatUint(typedData, 10))
	case []byte:
		w.writeBytes(typedData)
	case []Node:
		w.writeListStart(len(typedData))
		for _, n := range typedData {
			w.WriteNode(n)
		}
	default:
		panic(fmt.Errorf("%w: %T", ErrInvalidType, typedData))
	}
}

func (w *binaryEncoder) writeString(data string) {
	if !w.md && data == "c.us" {
		swnToken, _ := token.IndexOfSingleToken("s.whatsapp.net", false)
		w.pushByte(swnToken)
		return
	}

	tokenIndex, ok := token.IndexOfSingleToken(data, w.md)
	if ok {
		w.pushByte(tokenIndex)
		return
	}
	if w.md {
		var dictIndex byte
		dictIndex, tokenIndex, ok = token.IndexOfDoubleByteToken(data)
		if ok {
			w.pushByte(token.Dictionary0 + dictIndex)
			w.pushByte(tokenIndex)
			return
		}
	}
	if validateNibble(data) {
		w.writePackedBytes(data, token.Nibble8)
	} else if validateHex(data) {
		w.writePackedBytes(data, token.Hex8)
	} else {
		w.writeStringRaw(data)
	}
}

func (w *binaryEncoder) writeBytes(value []byte) {
	w.writeByteLength(len(value))
	w.pushBytes(value)
}

func (w *binaryEncoder) writeStringRaw(value string) {
	w.writeByteLength(len(value))
	w.pushString(value)
}

func (w *binaryEncoder) writeJID(jid FullJID) {
	if jid.AD {
		w.pushByte(token.ADJID)
		w.pushByte(jid.Agent)
		w.pushByte(jid.Device)
		w.writeString(jid.User)
	} else {
		w.pushByte(token.JIDPair)
		if len(jid.User) == 0 {
			w.pushByte(token.ListEmpty)
		} else {
			w.write(jid.User)
		}
		w.write(jid.Server)
	}
}

func (w *binaryEncoder) writeAttributes(attributes map[string]interface{}) {
	if attributes == nil {
		return
	}

	for key, val := range attributes {
		if val == "" {
			continue
		}

		w.writeString(key)
		w.write(val)
	}
}

func (w *binaryEncoder) writeListStart(listSize int) {
	if listSize == 0 {
		w.pushByte(byte(token.ListEmpty))
	} else if listSize < 256 {
		w.pushByte(byte(token.List8))
		w.pushInt8(listSize)
	} else {
		w.pushByte(byte(token.List16))
		w.pushInt16(listSize)
	}
}

func (w *binaryEncoder) writePackedBytes(value string, dataType int) {
	if len(value) > token.PackedMax {
		panic(fmt.Errorf("too many bytes to pack: %d", len(value)))
	}

	w.pushByte(byte(dataType))

	roundedLength := byte(math.Ceil(float64(len(value)) / 2.0))
	if len(value)%2 != 0 {
		roundedLength |= 128
	}
	w.pushByte(roundedLength)
	var packer func(byte) byte
	if dataType == token.Nibble8 {
		packer = packNibble
	} else if dataType == token.Hex8 {
		packer = packHex
	} else {
		// This should only be called with the correct values
		panic(fmt.Errorf("invalid packed byte data type %v", dataType))
	}
	for i, l := 0, len(value)/2; i < l; i++ {
		w.pushByte(w.packBytePair(packer, value[2*i], value[2*i+1]))
	}
	if len(value)%2 != 0 {
		w.pushByte(w.packBytePair(packer, value[len(value)-1], '\x00'))
	}
}

func (w *binaryEncoder) packBytePair(packer func(byte) byte, part1, part2 byte) byte {
	return (packer(part1) << 4) | packer(part2)
}

func validateNibble(value string) bool {
	if len(value) >= 128 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9') && char != '-' && char != '.' && char != '\x00' {
			return false
		}
	}
	return true
}

func packNibble(value byte) byte {
	switch value {
	case '-':
		return 10
	case '.':
		return 11
	case '\x00':
		return 15
	default:
		if value >= '0' && value <= '9' {
			return value - '0'
		}
		// This should be validated beforehand
		panic(fmt.Errorf("invalid string to pack as nibble: %s", string(value)))
	}
}

func validateHex(value string) bool {
	if len(value) >= 128 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9') && !(char >= 'A' && char <= 'F') && !(char >= 'a' && char <= 'a') && char != '\x00' {
			return false
		}
	}
	return true
}

func packHex(value byte) byte {
	switch {
	case value >= '0' && value <= '9':
		return value - '0'
	case value >= 'A' && value <= 'F':
		return value - 'A'
	case value >= 'a' && value <= 'f':
		return value - 'a'
	case value == '\x00':
		return 15
	default:
		// This should be validated beforehand
		panic(fmt.Errorf("invalid string to pack as hex: %s", string(value)))
	}
}
