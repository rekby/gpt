package gpt

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"hash/crc32"
)

const standardHeaderSize = 92 // Размер обычной GPT-структуры в байтах

// https://en.wikipedia.org/wiki/GUID_Partition_Table#Partition_table_header_.28LBA_1.29
type Header struct {
	Signature        [8]byte  // Offset  0. "EFI PART"
	Revision         uint32   // Offset  8
	Size             uint32   // Offset 12
	CRC              uint32   // Offset 16
	Reserved         uint32   // Offset 20
	CurrentLBA       uint64   // Offset 24
	OtherLBA         uint64   // Offset 32
	FirstUsableLBA   uint64   // Offset 40
	LastUsableLBA    uint64   // Offset 48
	GUID             [16]byte // Offset 56
	PartInfoStartLBA uint64   // Offset 72
	PartInfoArrLen   uint32   // Offset 80
	PartInfoSize     uint32   // Offset 84
	PartInfoCRC      uint32   // Offset 88
	TrailingBytes    []byte   // Offset 92
}

// Have to set to start of Header. Usually LBA1 for primary header.
func Load(reader io.Reader, sectorSize uint64) (res Header, err error) {
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
	read(&res.CurrentLBA)
	read(&res.OtherLBA)
	read(&res.FirstUsableLBA)
	read(&res.LastUsableLBA)
	read(&res.GUID)
	read(&res.PartInfoStartLBA)
	read(&res.PartInfoArrLen)
	read(&res.PartInfoSize)
	read(&res.PartInfoCRC)
	if err != nil {
		return
	}

	if res.Size > sectorSize || res.Signature < standardHeaderSize {
		return res, fmt.Errorf("Strange header size field: %v bytes", res.Size)
	}
	trailingBytes := make([]byte, sectorSize-standardHeaderSize)
	reader.Read(trailingBytes)
	res.TrailingBytes = trailingBytes
	return res, nil
}

func (this *Header) calcCRC()uint32{
	buf := bytes.Buffer{}
	this.Save(buf, false)
	return crc32.ChecksumIEEE(buf.Bytes()[:this.Size])
}

func (this *Header) Save(writer io.Writer, saveCRC bool) (err error) {
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
		write(&uint32(0))
	}

	write(&this.Reserved)
	write(&this.CurrentLBA)
	write(&this.OtherLBA)
	write(&this.FirstUsableLBA)
	write(&this.LastUsableLBA)
	write(&this.GUID)
	write(&this.PartInfoStartLBA)
	write(&this.PartInfoArrLen)
	write(&this.PartInfoSize)
	write(&this.PartInfoCRC)
	if err != nil {
		return
	}
	writer(this.TrailingBytes)
	return
}
