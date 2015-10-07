package gpt

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
)

const standardHeaderSize = 92          // Size of standard GPT-header in bytes
const standardPartitionEntrySize = 128 // Size of standard GPT-partition entry in bytes

// https://en.wikipedia.org/wiki/GUID_Partition_Table#Partition_table_header_.28LBA_1.29
type Header struct {
	Signature              [8]byte  // Offset  0. "EFI PART"
	Revision               uint32   // Offset  8
	Size                   uint32   // Offset 12
	CRC                    uint32   // Offset 16. Autocalc when save Header.
	Reserved               uint32   // Offset 20
	HeaderStartLBA         uint64   // Offset 24
	HeaderCopyStartLBA     uint64   // Offset 32
	FirstUsableLBA         uint64   // Offset 40
	LastUsableLBA          uint64   // Offset 48
	DiskGUID               [16]byte // Offset 56
	PartitionsTableStartLBA uint64   // Offset 72
	PartitionsArrLen       uint32   // Offset 80
	PartitionEntrySize     uint32   // Offset 84
	PartitionsCRC          uint32   // Offset 88. Autocalc when save Table.
	TrailingBytes          []byte   // Offset 92
}

// https://en.wikipedia.org/wiki/GUID_Partition_Table#Partition_entries
type Partition struct {
	Type          [16]byte // Offset 0
	PartGUID      [16]byte // Offset 16
	FirstLBA      uint64   // Offset 32
	LastLBA       uint64   // Offset 40
	Flags         [8]byte  // Offset 68
	PartNameUTF16 [72]byte // Offset 56
	TrailingBytes []byte   // Offset 128. Usually it is empty
}

type Table struct {
	SectorSize uint32 // in bytes
	Header     Header
	Partitions []Partition
}

//////////////////////////////////////////////
////////////////// HEADER ////////////////////
//////////////////////////////////////////////

// Have to set to start of Header. Usually LBA1 for primary header.
func readHeader(reader io.Reader, sectorSize uint32) (res Header, err error) {
	read := func(data interface{}) {
		if err == nil {
			err = binary.Read(reader, binary.LittleEndian, data)
		}
	}

	read(&res.Signature)
	read(&res.Revision)
	read(&res.Size)
	read(&res.CRC)
	read(&res.Reserved)
	read(&res.HeaderStartLBA)
	read(&res.HeaderCopyStartLBA)
	read(&res.FirstUsableLBA)
	read(&res.LastUsableLBA)
	read(&res.DiskGUID)
	read(&res.PartitionsTableStartLBA)
	read(&res.PartitionsArrLen)
	read(&res.PartitionEntrySize)
	read(&res.PartitionsCRC)
	if err != nil {
		return
	}

	if string(res.Signature[:]) != "EFI PART" {
		return res, fmt.Errorf("Bad GPT signature")
	}

	trailingBytes := make([]byte, sectorSize-standardHeaderSize)
	reader.Read(trailingBytes)
	res.TrailingBytes = trailingBytes

	if res.calcCRC() != res.CRC {
		return res, fmt.Errorf("BAD GPT Header CRC")
	}

	return
}

func (this *Header) calcCRC() uint32 {
	buf := &bytes.Buffer{}
	this.write(buf, false)
	return crc32.ChecksumIEEE(buf.Bytes()[:this.Size])
}

func (this *Header) write(writer io.Writer, saveCRC bool) (err error) {
	write := func(data interface{}) {
		if err == nil {
			err = binary.Write(writer, binary.LittleEndian, data)
		}
	}

	write(&this.Signature)
	write(&this.Revision)
	write(&this.Size)

	if saveCRC {
		this.CRC = this.calcCRC()
		write(&this.CRC)
	} else {
		write(uint32(0))
	}

	write(&this.Reserved)
	write(&this.HeaderStartLBA)
	write(&this.HeaderCopyStartLBA)
	write(&this.FirstUsableLBA)
	write(&this.LastUsableLBA)
	write(&this.DiskGUID)
	write(&this.PartitionsTableStartLBA)
	write(&this.PartitionsArrLen)
	write(&this.PartitionEntrySize)
	write(&this.PartitionsCRC)
	if err != nil {
		return
	}
	write(this.TrailingBytes)
	return
}

//////////////////////////////////////////////
///////////////// PARTITION //////////////////
//////////////////////////////////////////////
func readPartition(reader io.Reader, size uint32) (p Partition, err error) {
	read := func(data interface{}) {
		if err == nil {
			err = binary.Read(reader, binary.LittleEndian, data)
		}
	}

	p.TrailingBytes = make([]byte, size-standardPartitionEntrySize)

	read(&p.Type)
	read(&p.PartGUID)
	read(&p.FirstLBA)
	read(&p.LastLBA)
	read(&p.Flags)
	read(&p.PartNameUTF16)
	read(&p.TrailingBytes)

	return
}

func (this Partition) write(writer io.Writer, size uint32) (err error) {
	write := func(data interface{}) {
		if err == nil {
			err = binary.Write(writer, binary.LittleEndian, data)
		}
	}

	if size != uint32(standardPartitionEntrySize+len(this.TrailingBytes)) {
		return fmt.Errorf("Entry size(%v) != real entry size(%v)", size, standardPartitionEntrySize+len(this.TrailingBytes))
	}

	write(this.Type)
	write(this.PartGUID)
	write(this.FirstLBA)
	write(this.LastLBA)
	write(this.Flags)
	write(this.PartNameUTF16)
	write(this.TrailingBytes)

	return
}

func (this Partition) IsEmpty() bool {
	return this.Type == [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
}

//////////////////////////////////////////////
////////////////// TABLE /////////////////////
//////////////////////////////////////////////


// Read GPT partition
// Have to set to first byte of GPT Header (usually start of second sector on disk)
func ReadTable(reader io.ReadSeeker, SectorSize uint32) (table Table, err error) {
	table.SectorSize = SectorSize
	table.Header, err = readHeader(reader, SectorSize)
	if err != nil {
		return
	}
	if seekDest, ok := mul(int64(SectorSize), int64(table.Header.PartitionsTableStartLBA)); ok {
		reader.Seek(seekDest, 0)
	} else {
		err = fmt.Errorf("Seek overflow when read partition tables")
		return
	}
	for i := uint32(0); i < table.Header.PartitionsArrLen; i++ {
		var p Partition
		p, err = readPartition(reader, table.Header.PartitionEntrySize)
		if err != nil {
			return
		}
		table.Partitions = append(table.Partitions, p)
	}

	if table.Header.PartitionsCRC != table.calcPartitionsCRC() {
		err = fmt.Errorf("Bad partitions crc")
		return
	}
	return
}

func (this Table) calcPartitionsCRC() uint32 {
	buf := &bytes.Buffer{}
	for _, part := range this.Partitions {
		part.write(buf, this.Header.PartitionEntrySize)
	}
	return crc32.ChecksumIEEE(buf.Bytes())
}

// Calc header and partitions CRC. Save Header and partition entries to the disk.
// It independent of start position: writer will be seek to position from Table.Header.
func (this Table) Write(writer io.WriteSeeker) (err error) {
	this.Header.PartitionsCRC = this.calcPartitionsCRC()
	if headerPos, ok := mul(int64(this.SectorSize), int64(this.Header.HeaderStartLBA)); ok {
		writer.Seek(headerPos, 0)
	}
	err = this.Header.write(writer, true)
	if err != nil {
		return
	}
	if partTablePos, ok := mul(int64(this.SectorSize), int64(this.Header.PartitionsTableStartLBA)); ok {
		writer.Seek(partTablePos, 0)
	}
	for _, part := range this.Partitions {
		err = part.write(writer, this.Header.PartitionEntrySize)
		if err != nil {
			return
		}
	}
	return
}

//////////////////////////////////////////////
//////////////// INTERNALS ///////////////////
//////////////////////////////////////////////

// Multiply two int64 numbers with overflow check
// Algorithm from https://gist.github.com/areed/85d3614a58400e417027
func mul(a, b int64) (res int64, ok bool) {
	const mostPositive = 1<<63 - 1
	const mostNegative = -(mostPositive + 1)

	if a == 0 || b == 0 || a == 1 || b == 1 {
		return a * b, true
	}
	if a == mostNegative || b == mostNegative {
		return a * b, false
	}
	c := a * b
	return c, c/b == a
}
