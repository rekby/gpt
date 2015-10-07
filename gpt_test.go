package gpt

import (
	"bytes"
	"testing"
)

type randomWriteBuffer struct {
	buf    []byte
	offset int
}

func (this *randomWriteBuffer) Seek(offset int64, whence int) (newOffset int64, err error) {
	switch whence {
	case 0:
		this.offset = int(offset)
	case 1:
		this.offset += int(offset)
	case 2:
		this.offset = len(this.buf) + int(offset)
	default:
		panic("Error whence")
	}

	if this.offset >= len(this.buf) {
		newBuf := make([]byte, this.offset)
		copy(newBuf, this.buf)
		this.buf = newBuf
	}
	return int64(this.offset), nil
}

func (this*randomWriteBuffer) Write(p []byte) (n int, err error){
	needLen := this.offset + len(p)
	if needLen > len(this.buf) {
		newBuf := make([]byte, needLen)
		copy(newBuf, this.buf)
		this.buf = newBuf
	}
	copy(this.buf[this.offset:], p)
	this.offset += len(p)
	return len(p), nil
}


func TestHeaderRead(t *testing.T) {
	reader := bytes.NewReader(GPT_TEST_HEADER)
	h, err := readHeader(reader, 512)
	if err != nil {
		t.Errorf(err.Error())
	}
	if string(h.Signature[:]) != "EFI PART" {
		t.Error("Signature: ", string(h.Signature[:]))
	}
	if h.Revision != 0x00010000 { // v1.00 in hex
		t.Error("Revision: ", h.Revision)
	}
	if h.Size != 92 {
		t.Error("Header size: ", h.Size)
	}
	if h.CRC != h.calcCRC() {
		t.Error("CRC")
	}
	if h.Reserved != 0 {
		t.Error("Reserved")
	}
	if h.HeaderStartLBA != 1 {
		t.Error("CurrentLBA", h.HeaderStartLBA)
	}
	if h.HeaderCopyStartLBA != 1953525167 {
		t.Error("Other LBA", h.HeaderCopyStartLBA)
	}
	if h.FirstUsableLBA != 34 {
		t.Error("FirstUsable: ", h.FirstUsableLBA)
	}
	if h.LastUsableLBA != 1953525134 {
		t.Error("LastUsable: ", h.LastUsableLBA)
	}
	if !bytes.Equal(h.DiskGUID[:], []byte{190, 139, 78, 124, 58, 164, 159, 72, 142, 28, 5, 196, 90, 42, 168, 188}) {
		t.Error("Disk GUID: ", h.DiskGUID)
	}
	if h.PartitionsTableStartLBA != 2 {
		t.Error("Start partition entries: ", h.PartitionsTableStartLBA)
	}
	if h.PartitionsArrLen != 128 {
		t.Error("Partition arr len", h.PartitionsArrLen)
	}
	if h.PartitionEntrySize != 128 {
		t.Error("Partition entry size:", h.PartitionEntrySize)
	}
	if h.PartitionsCRC != 1233018821 {
		t.Error("Partitions CRC", h.PartitionsCRC)
	}
	if !bytes.Equal(h.TrailingBytes, make([]byte, 420)) {
		t.Error("Trailing bytes: ", h.TrailingBytes)
	}
}

func TestHeaderReadWrite(t *testing.T) {
	reader := bytes.NewReader(GPT_TEST_HEADER)
	h, err := readHeader(reader, 512)
	if err != nil {
		t.Errorf(err.Error())
	}
	writer := &bytes.Buffer{}
	h.write(writer, true)

	if !bytes.Equal(GPT_TEST_HEADER, writer.Bytes()) {
		t.Error("Read and write not equal")
	}
}

func TestEntryReadWrite(t *testing.T) {
	testEntry := make([]byte, 137)
	copy(testEntry, GPT_TEST_ENTRIES[0:128])
	testEntry[128] = 1
	testEntry[129] = 231
	testEntry[130] = 144
	testEntry[131] = 66
	testEntry[132] = 123
	testEntry[133] = 15
	testEntry[134] = 18
	testEntry[135] = 26
	testEntry[126] = 215

	p, err := readPartition(bytes.NewReader(testEntry), 137)
	if err != nil {
		t.Error(err)
	}

	writeBuf := &bytes.Buffer{}
	err = p.write(writeBuf, 137)
	if err != nil {
		t.Error(err)
	}
	if !bytes.Equal(testEntry, writeBuf.Bytes()) {
		t.Error("Read-write")
	}
}

func TestPartitionRead(t *testing.T) {
	p, err := readPartition(bytes.NewReader(GPT_TEST_ENTRIES), 128)
	if err != nil {
		t.Error(err)
	}

	if !bytes.Equal(p.Type[:], []byte{40, 115, 42, 193, 31, 248, 210, 17, 186, 75, 0, 160, 201, 62, 201, 59}) {
		// C12A7328-F81F-11D2-BA4B-00A0C93EC93B in little endian: first 3 parts in reverse order
		t.Error("Part type: ", p.Type)
	}
	if !bytes.Equal(p.PartGUID[:], []byte{176, 80, 47, 220, 222, 152, 129, 70, 168, 104, 66, 233, 254, 189, 110, 62}) {
		t.Error("Partition GUID: ", p.PartGUID)
	}
	if p.FirstLBA != 2048 {
		t.Error("First LBA: ", p.FirstLBA)
	}
	if p.LastLBA != 780287 {
		t.Error("Last LBA: ", p.LastLBA)
	}
	if !bytes.Equal(p.Flags[:], make([]byte, 8)) {
		t.Error("Flags: ", p.Flags)
	}
	if !bytes.Equal(p.PartNameUTF16[:], make([]byte, 72)) {
		t.Error("Name: ", p.PartNameUTF16)
	}
}

func TestPartitionBadWrite(t *testing.T) {
	var p Partition
	p.TrailingBytes = []byte{1, 2, 3}
	buf := &bytes.Buffer{}
	if p.write(buf, 130) == nil || p.write(buf, 132) == nil {
		t.Error("Write with bad entry size")
	}
	if p.write(buf, 131) != nil {
		t.Error("Write with ok entry size")
	}
}

func TestReadWriteTable(t *testing.T) {
	buf := make([]byte, 512+512+32*512)
	copy(buf[512:], GPT_TEST_HEADER)
	copy(buf[1024:], GPT_TEST_ENTRIES)
	reader := bytes.NewReader(buf)
	reader.Seek(512, 0)
	table, err := ReadTable(reader, 512)
	if err != nil {
		t.Error(err)
	}

	buf2 := &randomWriteBuffer{}
	table.Write(buf2)
	if !bytes.Equal(buf, buf2.buf) {
		t.Error("Bad read-write")
	}
}
