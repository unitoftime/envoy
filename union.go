package envoy

import (
	"fmt"
	"reflect"
	"errors"
	"encoding/binary"

	// "github.com/unitoftime/binary"
)

var ErrUvarintCorrupted = errors.New("failed to read uvarint is corrupted")

type Buffer struct {
	buf []byte
	off int
}
func NewBuffer(length int) *Buffer {
	return &Buffer{
		buf: make([]byte, 0, length),
		// endian: binary.LittleEndian, // TODO: Made things a bit slower. retest at scale
	}
}

func NewBufferFrom(dat []byte) *Buffer {
	return &Buffer{
		buf: dat,
		// endian: binary.LittleEndian, // TODO: Made things a bit slower. retest at scale
	}
}

func (b *Buffer) Reset() {
	b.off = 0
	b.buf = b.buf[:0]
}

// Retursn all of the bytes in the buffer
func (b *Buffer) Bytes() []byte {
	return b.buf
}

func (b *Buffer) ReadUint8() uint8 {
	if len(b.buf) < 1 { panic("AA") }
	ret := b.buf[b.off]
	b.off += 1
	return ret
}
func (b *Buffer) ReadUint32() (uint32, error) {
	v, err := b.ReadUint64()
	return uint32(v), err
}
func (b *Buffer) ReadUint64() (uint64, error) {
	ret, n := binary.Uvarint(b.buf[b.off:])
	if n <= 0 {
		return 0, ErrUvarintCorrupted
	}
	b.off += n
	return ret, nil
}
func (b *Buffer) WriteUint8(v uint8) *Buffer {
	b.buf = append(b.buf, v)
	return b
}
func (b *Buffer) WriteUint32(v uint32) *Buffer {
	b.buf = binary.AppendUvarint(b.buf, uint64(v))
	return b
}



// type Union struct {
// 	value any
// }
// func (u Union) GetRawValue() any {
// 	return u.value
// }
// func (u *Union) PutRawValue(v any) {
// 	u.value = v
// }

// type Union struct {
// 	tag uint64
// 	hidden Er
// }

// func (u *Union) EncodeCod(buf *Buffer) {
// 	buf.WriteUint64(u.tag)
// 	buf.Wr
// }

// func (u *Union) DecodeCod(buf *Buffer) error {

// }

// type Union struct {
// 	Type uint8
// 	Payload []byte
// }

type NewEncoder interface {
	EncodeCod([]byte) []byte
}
type NewDecoder interface {
	DecodeCod([]byte) (int, error)
}


type UnionBuilder struct {
	types map[reflect.Type]uint8
	impl []any
}

func NewUnion(structs ...any) *UnionBuilder {
	if len(structs) > 256 {
		panic("TOO MANY STRUCTS")
	}

	types := make(map[reflect.Type]uint8)
	for i := range structs {
		typeStr := reflect.TypeOf(structs[i])
		types[typeStr] = uint8(i)
	}

	return &UnionBuilder {
		types: types,
		impl: structs,
	}
}

// Converts the underlying value inside the to a pointer and returns an interface for that
func valToPtr(val any) any {
	v := reflect.ValueOf(val)
	rVal := reflect.New(v.Type())
	rVal.Elem().Set(v)
	ptrVal := rVal.Interface()
	return ptrVal
}
// Converts the underlying interface with pointer to just the value
func ptrToVal(valPtr any) any {
	return reflect.Indirect(reflect.ValueOf(valPtr)).Interface()
}

// func (u *UnionBuilder) Make(buf *Buffer, val any) error {
// 	typeStr := reflect.TypeOf(val)
// 	typeId, ok := u.types[typeStr]
// 	if !ok {
// 		return fmt.Errorf("Unknown Type: %T", val)
// 	}

// 	// TODO - can optimize the double serialize
// 	serializedVal, err := binary.Marshal(val)
// 	if err != nil { return err } // TODO: get rid of
// 	// fmt.Printf("%T: %d\n", val, len(serializedVal))

// 	buf.WriteUint8(typeId)
// 	buf.WriteByteSlice(serializedVal)

// 	return nil
// }
// func (u *UnionBuilder) Make(val any) (Union, error) {
// 	typeStr := reflect.TypeOf(val)
// 	typeId, ok := u.types[typeStr]
// 	if !ok {
// 		return Union{}, fmt.Errorf("Unknown Type: %T", val)
// 	}

// 	// TODO - can optimize the double serialize
// 	serializedVal, err := binary.Marshal(val)
// 	if err != nil {
// 		return Union{}, err
// 	}
// 	// fmt.Printf("%T: %d\n", val, len(serializedVal))

// 	union := Union{
// 		Type: typeId,
// 		Payload: serializedVal,
// 	}
// 	return union, nil
// }

// func (u *UnionBuilder) Unmake(union Union) (any, error) {
// 	idx := int(union.Type)
// 	if idx >= len(u.impl) {
// 		return nil, fmt.Errorf("Unknown message opcode %d max: %d", idx, len(u.impl)-1)
// 	}
// 	val := u.impl[idx]
// 	valPtr := valToPtr(val)

// 	err := binary.Unmarshal(union.Payload, valPtr)

// 	return ptrToVal(valPtr), err
// }

func (u *UnionBuilder) Serialize(buf *Buffer, val any) error {
	// union, err := u.Make(val)
	// if err != nil {
	// 	return err
	// }

	// dat, err := binary.Marshal(union)
	// if err != nil { return err }
	// buf.WriteByteSlice(dat)
	// return nil

	typeStr := reflect.TypeOf(val)
	typeId, ok := u.types[typeStr]
	if !ok {
		return fmt.Errorf("Unknown Type: %T", val)
	}

	// serializedVal, err := binary.Marshal(val)
	// if err != nil { return err } // TODO: get rid of
	// fmt.Printf("%T: %d\n", val, len(serializedVal))

	buf.WriteUint8(typeId)
	// buf.WriteByteSlice(serializedVal)

	buf.buf = val.(NewEncoder).EncodeCod(buf.buf)

	// buf.WriteAny(val)

	return nil
}

func (u *UnionBuilder) Deserialize(buf *Buffer) (any, error) {
	// dat, err := buf.ReadByteSlice()
	// if err != nil { return nil, err }

	// union := Union{}
	// err = binary.Unmarshal(dat, &union)
	// if err != nil { return nil, err }

	// return u.Unmake(union)

	typeId := buf.ReadUint8()
	// if err != nil { return nil, err }
	// dat, err := buf.ReadByteSlice()
	// if err != nil { return nil, err }

	idx := int(typeId)
	if idx >= len(u.impl) {
		return nil, fmt.Errorf("Unknown message opcode %d max: %d", idx, len(u.impl)-1)
	}
	val := u.impl[idx]
	valPtr := valToPtr(val)

	n, err := valPtr.(NewDecoder).DecodeCod(buf.buf[buf.off:])
	if err != nil { return nil, err }
	buf.off += n

	// err = binary.Unmarshal(dat, valPtr)
	// err := buf.ReadAny(valPtr)

	return ptrToVal(valPtr), err
}
