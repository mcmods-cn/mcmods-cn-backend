package httpapi

import (
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"io"
	"os"
	"runtime"
	"testing"
)

type fixedByteReader struct {
	remaining int64
	value     byte
}

func (reader *fixedByteReader) Read(target []byte) (int, error) {
	if reader.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(target)) > reader.remaining {
		target = target[:reader.remaining]
	}
	for index := range target {
		target[index] = reader.value
	}
	reader.remaining -= int64(len(target))
	return len(target), nil
}

func TestSpoolProviderFileKeepsHeapBoundedNearMaximumSize(t *testing.T) {
	size := projectAutomationMaxFileBytes - projectAutomationMirrorBufferBytes
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	spooled, err := spoolProviderFile(&fixedByteReader{remaining: size, value: 0x5a}, providerProjectFile{SizeBytes: size}, projectAutomationMaxFileBytes)
	if err != nil {
		t.Fatal(err)
	}
	path := spooled.Path
	if spooled.Size != size || len(spooled.SHA256) != 64 {
		spooled.Close()
		t.Fatalf("spooled size=%d digest=%q", spooled.Size, spooled.SHA256)
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 16<<20 {
		spooled.Close()
		t.Fatalf("near-limit stream allocated %d heap bytes", allocated)
	} else {
		t.Logf("streamed %d bytes with %d total heap bytes allocated", size, allocated)
	}
	buffer := make([]byte, 1)
	if _, err = spooled.File.Read(buffer); err != nil || buffer[0] != 0x5a {
		spooled.Close()
		t.Fatalf("read spooled content: value=%x err=%v", buffer[0], err)
	}
	spooled.Close()
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temporary mirror file remains after close: %v", err)
	}
}

func TestSpoolProviderFileValidatesEveryHashFromTheSameStream(t *testing.T) {
	payload := []byte("bounded provider mirror payload")
	wantSHA1 := sha1.Sum(payload)
	wantSHA256 := sha256.Sum256(payload)
	wantSHA512 := sha512.Sum512(payload)
	file := providerProjectFile{
		SizeBytes: int64(len(payload)), SHA1: hex.EncodeToString(wantSHA1[:]), SHA512: hex.EncodeToString(wantSHA512[:]),
	}
	spooled, err := spoolProviderFile(&fixedByteReader{remaining: int64(len(payload)), value: payload[0]}, file, 1024)
	if err == nil {
		spooled.Close()
		t.Fatal("different streamed content unexpectedly matched supplied hashes")
	}

	spooled, err = spoolProviderFile(&byteSliceReader{value: payload}, file, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer spooled.Close()
	if spooled.SHA256 != hex.EncodeToString(wantSHA256[:]) {
		t.Fatalf("SHA-256=%s", spooled.SHA256)
	}
}

type byteSliceReader struct {
	value []byte
	read  bool
}

func (reader *byteSliceReader) Read(target []byte) (int, error) {
	if reader.read {
		return 0, io.EOF
	}
	reader.read = true
	return copy(target, reader.value), nil
}

func TestSpoolProviderFileRejectsTheFirstBytePastItsLimit(t *testing.T) {
	if spooled, err := spoolProviderFile(&fixedByteReader{remaining: 1025, value: 1}, providerProjectFile{}, 1024); err == nil {
		spooled.Close()
		t.Fatal("over-limit provider file was accepted")
	}
}
