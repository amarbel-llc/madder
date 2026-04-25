//go:build linux

// POC 0001: userfaultfd-driven lazy mmap. See README.md.
package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/klauspost/compress/zstd"
	"golang.org/x/sys/unix"
)

const (
	totalBytes = 64 << 20 // 64 MiB
	chunkBytes = 1 << 20  // 1 MiB
	numChunks  = totalBytes / chunkBytes

	verifyReads = 4096

	uffdAPIMagic            = 0xAA
	uffdUserModeOnly        = 1
	uffdioAPI               = 0xC018AA3F
	uffdioRegister          = 0xC020AA00
	uffdioCopy              = 0xC028AA03
	uffdioRegModeMissing    = 1
	uffdEventPagefault uint8 = 0x12
)

type uffdioAPIArg struct {
	api      uint64
	features uint64
	ioctls   uint64
}

type uffdioRegisterArg struct {
	rangeStart uint64
	rangeLen   uint64
	mode       uint64
	ioctls     uint64
}

type uffdioCopyArg struct {
	dst    uint64
	src    uint64
	length uint64
	mode   uint64
	copied int64
}

// Direct cast of the 32-byte uffd_msg to the pagefault arm.
type uffdMsg struct {
	event   uint8
	_       uint8
	_       uint16
	_       uint32
	flags   uint64
	address uint64
	tid     uint64
}

// splitmix64Byte returns the low byte of splitmix64 applied to i.
// Verifiable from any offset without state — that's the whole point.
func splitmix64Byte(i uint64) byte {
	h := i
	h ^= h >> 30
	h *= 0xbf58476d1ce4e5b9
	h ^= h >> 27
	h *= 0x94d049bb133111eb
	h ^= h >> 31
	return byte(h)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "POC FAILED:", err)
		os.Exit(1)
	}
}

func run() error {
	pageSize := os.Getpagesize()
	if chunkBytes%pageSize != 0 {
		return fmt.Errorf("chunkBytes %d not multiple of page size %d", chunkBytes, pageSize)
	}

	dir, err := os.MkdirTemp("", "uffdpoc-*")
	if err != nil {
		return fmt.Errorf("mktemp: %w", err)
	}
	defer os.RemoveAll(dir)

	fmt.Printf("kernel page size: %d bytes\n", pageSize)
	fmt.Printf("fixture dir:      %s\n", dir)

	tFix := time.Now()
	chunkOffsets, err := writeFixture(dir)
	if err != nil {
		return fmt.Errorf("writeFixture: %w", err)
	}
	fmt.Printf("fixture: %d MiB plaintext, %d chunks, compressed=%d bytes (gen %s)\n",
		totalBytes>>20, numChunks, chunkOffsets[numChunks], time.Since(tFix).Round(time.Millisecond))

	zfile, err := os.Open(filepath.Join(dir, "payload.zchunks"))
	if err != nil {
		return fmt.Errorf("open zchunks: %w", err)
	}
	defer zfile.Close()

	mmapBytes, err := unix.Mmap(-1, 0, totalBytes,
		unix.PROT_READ|unix.PROT_WRITE,
		unix.MAP_PRIVATE|unix.MAP_ANONYMOUS)
	if err != nil {
		return fmt.Errorf("mmap: %w", err)
	}
	defer func() { _ = unix.Munmap(mmapBytes) }()

	base := uintptr(unsafe.Pointer(&mmapBytes[0]))

	uffd, err := openUserfaultfd()
	if err != nil {
		return fmt.Errorf("userfaultfd: %w", err)
	}
	defer unix.Close(uffd)

	if err := callUffdioAPI(uffd); err != nil {
		return fmt.Errorf("UFFDIO_API: %w", err)
	}
	if err := callUffdioRegister(uffd, base, totalBytes); err != nil {
		return fmt.Errorf("UFFDIO_REGISTER: %w", err)
	}
	fmt.Printf("uffd registered: base=0x%x len=%d\n", base, totalBytes)

	var (
		populated      atomic.Uint64
		faultsHandled  atomic.Uint64
		decompressions atomic.Uint64
		handlerReady   = make(chan struct{})
		handlerDone    = make(chan error, 1)
	)

	stop := &atomic.Bool{}
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		handlerDone <- handleFaults(uffd, base, zfile, chunkOffsets,
			handlerReady, stop, &populated, &faultsHandled, &decompressions)
	}()
	<-handlerReady
	fmt.Println("handler ready")

	rng := rand.New(rand.NewPCG(0xfeedface, 0xc0ffee))
	tVerify := time.Now()
	for i := 0; i < verifyReads; i++ {
		off := rng.Uint64N(totalBytes)
		got := mmapBytes[off]
		want := splitmix64Byte(off)
		if got != want {
			return fmt.Errorf("verify offset %d: got 0x%02x want 0x%02x", off, got, want)
		}
	}
	verifyDur := time.Since(tVerify)

	runtime.KeepAlive(mmapBytes)

	stop.Store(true)
	if err := <-handlerDone; err != nil && !errors.Is(err, errHandlerStopped) {
		return fmt.Errorf("handler: %w", err)
	}

	chunksTouched := bits.OnesCount64(populated.Load())
	fmt.Printf("verify: %d random reads in %s\n", verifyReads, verifyDur.Round(time.Microsecond))
	fmt.Printf("faults handled: %d\n", faultsHandled.Load())
	fmt.Printf("decompressions: %d (%d / %d chunks touched)\n",
		decompressions.Load(), chunksTouched, numChunks)
	fmt.Println("POC OK")
	return nil
}

func writeFixture(dir string) ([]uint64, error) {
	zfilePath := filepath.Join(dir, "payload.zchunks")
	idxPath := filepath.Join(dir, "payload.idx")

	zfile, err := os.Create(zfilePath)
	if err != nil {
		return nil, err
	}
	defer zfile.Close()

	enc, err := zstd.NewWriter(nil)
	if err != nil {
		return nil, err
	}
	defer enc.Close()

	plaintext := make([]byte, chunkBytes)
	offsets := make([]uint64, numChunks+1)
	var written uint64

	for c := uint64(0); c < numChunks; c++ {
		base := c * chunkBytes
		for i := 0; i < chunkBytes; i++ {
			plaintext[i] = splitmix64Byte(base + uint64(i))
		}
		offsets[c] = written
		compressed := enc.EncodeAll(plaintext, nil)
		n, err := zfile.Write(compressed)
		if err != nil {
			return nil, err
		}
		written += uint64(n)
	}
	offsets[numChunks] = written

	if err := zfile.Sync(); err != nil {
		return nil, err
	}

	idxFile, err := os.Create(idxPath)
	if err != nil {
		return nil, err
	}
	defer idxFile.Close()
	for _, off := range offsets {
		if err := binary.Write(idxFile, binary.LittleEndian, off); err != nil {
			return nil, err
		}
	}
	return offsets, nil
}

func openUserfaultfd() (int, error) {
	r, _, errno := unix.Syscall(unix.SYS_USERFAULTFD,
		uintptr(uffdUserModeOnly|unix.O_CLOEXEC|unix.O_NONBLOCK), 0, 0)
	if errno != 0 {
		return 0, errno
	}
	return int(r), nil
}

func callUffdioAPI(fd int) error {
	api := uffdioAPIArg{api: uffdAPIMagic}
	_, _, errno := unix.Syscall(unix.SYS_IOCTL,
		uintptr(fd), uintptr(uffdioAPI), uintptr(unsafe.Pointer(&api)))
	if errno != 0 {
		return errno
	}
	return nil
}

func callUffdioRegister(fd int, addr uintptr, length int) error {
	reg := uffdioRegisterArg{
		rangeStart: uint64(addr),
		rangeLen:   uint64(length),
		mode:       uffdioRegModeMissing,
	}
	_, _, errno := unix.Syscall(unix.SYS_IOCTL,
		uintptr(fd), uintptr(uffdioRegister), uintptr(unsafe.Pointer(&reg)))
	if errno != 0 {
		return errno
	}
	return nil
}

var errHandlerStopped = errors.New("handler stopped by main")

func handleFaults(
	fd int,
	base uintptr,
	zfile *os.File,
	chunkOffsets []uint64,
	ready chan<- struct{},
	stop *atomic.Bool,
	populated *atomic.Uint64,
	faultsHandled *atomic.Uint64,
	decompressions *atomic.Uint64,
) error {
	dec, err := zstd.NewReader(nil)
	if err != nil {
		close(ready)
		return fmt.Errorf("zstd reader: %w", err)
	}
	defer dec.Close()

	scratch := make([]byte, 0, chunkBytes)
	compressedBuf := make([]byte, 0, 256<<10)

	var msg uffdMsg
	msgBytes := unsafe.Slice((*byte)(unsafe.Pointer(&msg)), unsafe.Sizeof(msg))

	pollFds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}

	close(ready)

	for {
		if stop.Load() {
			return errHandlerStopped
		}
		// Block up to 100ms waiting for an event, then re-check stop.
		_, err := unix.Poll(pollFds, 100)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return fmt.Errorf("poll uffd: %w", err)
		}
		if pollFds[0].Revents&unix.POLLIN == 0 {
			continue
		}
		n, err := unix.Read(fd, msgBytes)
		if err != nil {
			if errors.Is(err, unix.EAGAIN) {
				continue
			}
			if errors.Is(err, unix.EBADF) || errors.Is(err, unix.EINVAL) {
				return errHandlerStopped
			}
			return fmt.Errorf("read uffd: %w", err)
		}
		if n == 0 {
			return errHandlerStopped
		}
		if n != int(unsafe.Sizeof(msg)) {
			return fmt.Errorf("short uffd read: %d", n)
		}
		if msg.event != uffdEventPagefault {
			return fmt.Errorf("unexpected uffd event: 0x%x", msg.event)
		}

		faultsHandled.Add(1)

		offsetInMap := uint64(msg.address) - uint64(base)
		chunkIdx := offsetInMap / chunkBytes
		if chunkIdx >= numChunks {
			return fmt.Errorf("fault outside region: addr=0x%x", msg.address)
		}

		bit := uint64(1) << chunkIdx
		if populated.Load()&bit != 0 {
			continue
		}

		zStart := chunkOffsets[chunkIdx]
		zEnd := chunkOffsets[chunkIdx+1]
		zLen := int(zEnd - zStart)
		if cap(compressedBuf) < zLen {
			compressedBuf = make([]byte, zLen)
		} else {
			compressedBuf = compressedBuf[:zLen]
		}
		if _, err := zfile.ReadAt(compressedBuf, int64(zStart)); err != nil {
			return fmt.Errorf("readat chunk %d: %w", chunkIdx, err)
		}

		out, err := dec.DecodeAll(compressedBuf, scratch[:0])
		if err != nil {
			return fmt.Errorf("decompress chunk %d: %w", chunkIdx, err)
		}
		if len(out) != chunkBytes {
			return fmt.Errorf("chunk %d: got %d bytes want %d", chunkIdx, len(out), chunkBytes)
		}
		decompressions.Add(1)

		dst := base + uintptr(chunkIdx)*chunkBytes
		copyArg := uffdioCopyArg{
			dst:    uint64(dst),
			src:    uint64(uintptr(unsafe.Pointer(&out[0]))),
			length: chunkBytes,
		}
		_, _, errno := unix.Syscall(unix.SYS_IOCTL,
			uintptr(fd), uintptr(uffdioCopy), uintptr(unsafe.Pointer(&copyArg)))
		if errno != 0 {
			return fmt.Errorf("UFFDIO_COPY chunk %d: %w", chunkIdx, errno)
		}
		if copyArg.copied != chunkBytes {
			return fmt.Errorf("UFFDIO_COPY chunk %d: copied %d want %d",
				chunkIdx, copyArg.copied, chunkBytes)
		}

		populated.Or(bit)
		runtime.KeepAlive(out)
	}
}
