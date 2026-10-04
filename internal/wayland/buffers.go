package wayland

import (
	"errors"
	"fmt"

	"github.com/Nomadcxx/sysc-terminal/internal/cell"
	"github.com/Nomadcxx/sysc-terminal/internal/raster"
	"github.com/Nomadcxx/sysc-wayland/client"
	"golang.org/x/sys/unix"
)

const (
	bufferSlots    = 2
	formatARGB8888 = uint32(client.ShmFormatArgb8888)
)

type bufferGeneration struct {
	fd       int
	data     []byte
	pool     *client.ShmPool
	buffers  [bufferSlots]*client.Buffer
	busy     [bufferSlots]bool
	lastGrid [bufferSlots]*cell.Grid
	width    int
	height   int
	stride   int
	retired  bool
	owner    *Owner
}

func newBufferGeneration(owner *Owner, shm *client.Shm, width, height int) (*bufferGeneration, error) {
	stride, total, err := raster.BufferSize(width, height, bufferSlots)
	if err != nil {
		return nil, err
	}
	fd, err := unix.MemfdCreate("sysc-terminal", unix.MFD_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("wayland: memfd_create: %w", err)
	}
	g := &bufferGeneration{fd: fd, width: width, height: height, stride: stride, owner: owner}
	if err := unix.Ftruncate(fd, int64(total)); err != nil {
		g.destroy()
		return nil, fmt.Errorf("wayland: ftruncate shm: %w", err)
	}
	g.data, err = unix.Mmap(fd, 0, total, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		g.destroy()
		return nil, fmt.Errorf("wayland: mmap shm: %w", err)
	}
	g.pool, err = shm.CreatePool(fd, int32(total))
	if err != nil {
		g.destroy()
		return nil, fmt.Errorf("wayland: create shm pool: %w", err)
	}
	slotBytes := stride * height
	for slot := range g.buffers {
		buffer, err := g.pool.CreateBuffer(int32(slot*slotBytes), int32(width), int32(height), int32(stride), formatARGB8888)
		if err != nil {
			g.destroy()
			return nil, fmt.Errorf("wayland: create shm buffer %d: %w", slot, err)
		}
		g.buffers[slot] = buffer
		buffer.SetReleaseHandler(func(client.BufferReleaseEvent) {
			g.busy[slot] = false
			g.owner.released = true
			g.owner.Wake()
		})
	}
	return g, nil
}

func (g *bufferGeneration) pixels(slot int) []byte {
	size := g.stride * g.height
	start := slot * size
	return g.data[start : start+size]
}

func (g *bufferGeneration) freeSlot() int {
	for slot := range g.buffers {
		if g.buffers[slot] != nil && !g.busy[slot] {
			return slot
		}
	}
	return -1
}

func (g *bufferGeneration) inUse() bool { return g.busy[0] || g.busy[1] }

func (g *bufferGeneration) resetHistory() { g.lastGrid = [bufferSlots]*cell.Grid{} }

func (g *bufferGeneration) destroy() error {
	var errs []error
	for slot, buffer := range g.buffers {
		if buffer != nil {
			if err := buffer.Destroy(); err != nil {
				errs = append(errs, fmt.Errorf("destroy shm buffer %d: %w", slot, err))
			}
			g.buffers[slot] = nil
		}
	}
	if g.pool != nil {
		if err := g.pool.Destroy(); err != nil {
			errs = append(errs, fmt.Errorf("destroy shm pool: %w", err))
		}
		g.pool = nil
	}
	if g.data != nil {
		if err := unix.Munmap(g.data); err != nil {
			errs = append(errs, fmt.Errorf("munmap shm: %w", err))
		}
		g.data = nil
	}
	if g.fd >= 0 {
		if err := unix.Close(g.fd); err != nil {
			errs = append(errs, fmt.Errorf("close shm fd: %w", err))
		}
		g.fd = -1
	}
	return errors.Join(errs...)
}
