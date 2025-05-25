package datastore

import (
	"bufio"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
)

type entry struct {
	key      string
	value    string
	checksum [20]byte
}

const (
	headerSize      = 4
	keyLengthSize   = 4
	valueLengthSize = 4
	checksumSize    = 20
	totalHeaderSize = headerSize + keyLengthSize + valueLengthSize + checksumSize
)

func calculateEntryLength(key, value string) int64 {
	return int64(len(key) + len(value) + totalHeaderSize)
}

func (e *entry) GetLength() int64 {
	return calculateEntryLength(e.key, e.value)
}

func (e *entry) calculateChecksum() [20]byte {
	return sha1.Sum([]byte(e.value))
}

func (e *entry) verifyChecksum() error {
	expectedChecksum := sha1.Sum([]byte(e.value))
	if expectedChecksum != e.checksum {
		return fmt.Errorf("checksum mismatch: data corruption detected for key '%s'", e.key)
	}
	return nil
}

func (e *entry) Decode(data []byte) {
	keyLength := binary.LittleEndian.Uint32(data[headerSize:])

	keyStart := headerSize + keyLengthSize
	keyEnd := keyStart + int(keyLength)

	keyBytes := make([]byte, keyLength)
	copy(keyBytes, data[keyStart:keyEnd])
	e.key = string(keyBytes)

	valueStart := keyEnd
	valueLength := binary.LittleEndian.Uint32(data[valueStart:])

	valueDataStart := valueStart + valueLengthSize
	valueDataEnd := valueDataStart + int(valueLength)

	valueBytes := make([]byte, valueLength)
	copy(valueBytes, data[valueDataStart:valueDataEnd])
	e.value = string(valueBytes)

	checksumStart := valueDataEnd
	copy(e.checksum[:], data[checksumStart:checksumStart+checksumSize])
}

func readValue(reader *bufio.Reader) (string, error) {
	headerBytes, err := reader.Peek(headerSize + keyLengthSize)
	if err != nil {
		return "", err
	}

	keySize := int(binary.LittleEndian.Uint32(headerBytes[headerSize:]))

	bytesToSkip := headerSize + keyLengthSize + keySize
	_, err = reader.Discard(bytesToSkip)
	if err != nil {
		return "", err
	}

	valueSizeBytes, err := reader.Peek(valueLengthSize)
	if err != nil {
		return "", err
	}

	valueSize := int(binary.LittleEndian.Uint32(valueSizeBytes))

	_, err = reader.Discard(valueLengthSize)
	if err != nil {
		return "", err
	}

	valueData := make([]byte, valueSize)
	bytesRead, err := reader.Read(valueData)
	if err != nil {
		return "", err
	}

	if bytesRead != valueSize {
		return "", fmt.Errorf("incomplete value read: got %d bytes, expected %d", bytesRead, valueSize)
	}

	var storedChecksum [20]byte
	checksumBytesRead, err := reader.Read(storedChecksum[:])
	if err != nil {
		return "", fmt.Errorf("failed to read checksum: %w", err)
	}

	if checksumBytesRead != checksumSize {
		return "", fmt.Errorf("incomplete checksum read: got %d bytes, expected %d", checksumBytesRead, checksumSize)
	}

	expectedChecksum := sha1.Sum(valueData)
	if expectedChecksum != storedChecksum {
		return "", fmt.Errorf("checksum mismatch: data corruption detected")
	}

	return string(valueData), nil
}

func (e *entry) Encode() []byte {
	e.checksum = e.calculateChecksum()

	keyLength := len(e.key)
	valueLength := len(e.value)
	totalSize := keyLength + valueLength + totalHeaderSize

	buffer := make([]byte, totalSize)

	binary.LittleEndian.PutUint32(buffer, uint32(totalSize))

	binary.LittleEndian.PutUint32(buffer[headerSize:], uint32(keyLength))

	copy(buffer[headerSize+keyLengthSize:], e.key)

	valueStart := headerSize + keyLengthSize + keyLength
	binary.LittleEndian.PutUint32(buffer[valueStart:], uint32(valueLength))

	copy(buffer[valueStart+valueLengthSize:], e.value)

	checksumStart := valueStart + valueLengthSize + valueLength
	copy(buffer[checksumStart:], e.checksum[:])

	return buffer
}
