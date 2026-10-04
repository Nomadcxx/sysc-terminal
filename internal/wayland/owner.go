package wayland

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Nomadcxx/sysc-terminal/internal/effect"
	"github.com/Nomadcxx/sysc-terminal/internal/raster"
	"github.com/Nomadcxx/sysc-terminal/internal/wayland/fractionalscale"
	"github.com/Nomadcxx/sysc-terminal/internal/wayland/layershell"
	"github.com/Nomadcxx/sysc-terminal/internal/wayland/viewporter"
	"github.com/Nomadcxx/sysc-wayland/client"
	"golang.org/x/sys/unix"
)

const (
	maxWaylandVersion = 6
	controlQueueSize  = 16
	startupTimeout    = 5 * time.Second
)

var (
	ErrOwnerBusy   = errors.New("wayland: control queue is full")
	ErrOwnerClosed = errors.New("wayland: owner is closed")
)

type Config struct {
	Output  string
	Effect  string
	Theme   string
	Artwork string
	Font    string
}

type ControlOp uint8

const (
	ControlStatus ControlOp = iota
	ControlPause
	ControlChange
	ControlReset
	ControlStop
)

type ControlRequest struct {
	Op     ControlOp
	ID     string
	Theme  string
	File   string
	Paused bool
	Reply  chan ControlReply
}

type ControlReply struct {
	Playing bool
	ID      string
	Theme   string
	Err     error
}

type outputBinding struct {
	global   uint32
	version  uint32
	proxy    *client.Output
	name     string
	scale120 int
}

type pendingConfigure struct {
	width  int
	height int
}

type layoutRequest struct {
	width    int
	height   int
	scale120 int
}

type Owner struct {
	cfg        Config
	worker     *effectWorker
	rasterizer *raster.Rasterizer

	controls   chan ControlRequest
	done       chan struct{}
	finishOnce sync.Once
	started    atomic.Bool
	wakeMu     sync.Mutex
	wakeRead   int
	wakeWrite  int

	display        *client.Display
	ctx            *client.Context
	registry       *client.Registry
	compositor     *client.Compositor
	shm            *client.Shm
	layerShell     *layershell.ZwlrLayerShellV1
	viewporter     *viewporter.WpViewporter
	scaleManager   *fractionalscale.WpFractionalScaleManagerV1
	outputs        map[uint32]*outputBinding
	selected       *outputBinding
	selectedGlobal uint32
	argb           bool
	bindErr        error
	fatal          error
	displayLost    bool

	surface       *client.Surface
	layer         *layershell.ZwlrLayerSurfaceV1
	viewport      *viewporter.WpViewport
	fractional    *fractionalscale.WpFractionalScaleV1
	frameCallback *client.Callback

	configure      *pendingConfigure
	preferredScale bool
	scale120       int
	logicalWidth   int
	logicalHeight  int
	geometry       GridGeometry
	configured     bool
	pendingLayout  *layoutRequest
	current        *bufferGeneration
	retired        []*bufferGeneration
	released       bool
	latest         workerResult
	hasLatest      bool
	pendingPaint   bool

	effectID    string
	theme       string
	paused      bool
	lastTick    time.Time
	tickPending bool
	pending     map[chan workerAck]chan ControlReply
	fatalOnAck  map[chan workerAck]bool
	stopping    bool
	ready       bool
}

func NewOwner(cfg Config) (*Owner, error) {
	if cfg.Output == "" {
		return nil, errors.New("wayland: output is required")
	}
	if cfg.Effect == "" {
		cfg.Effect = "fire"
	}
	if cfg.Theme == "" {
		cfg.Theme = "nord"
	}
	var initial *effect.Effect
	var err error
	if cfg.Artwork != "" {
		initial, err = effect.NewFromFile(cfg.Effect, cfg.Theme, effect.MinimumCols, effect.MinimumRows, cfg.Artwork)
	} else {
		initial, err = effect.New(cfg.Effect, cfg.Theme, effect.MinimumCols, effect.MinimumRows, "")
	}
	if err != nil {
		return nil, err
	}
	fontPath, err := findFont(cfg.Font)
	if err != nil {
		return nil, err
	}
	rz, err := raster.Open(fontPath, 12)
	if err != nil {
		return nil, fmt.Errorf("wayland: open font %q: %w", fontPath, err)
	}
	fds := []int{0, 0}
	if err := unix.Pipe2(fds, unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		return nil, fmt.Errorf("wayland: wake pipe: %w", err)
	}
	o := &Owner{
		cfg: cfg, rasterizer: rz, controls: make(chan ControlRequest, controlQueueSize),
		done: make(chan struct{}), wakeRead: fds[0], wakeWrite: fds[1],
		outputs: make(map[uint32]*outputBinding), scale120: 120,
		effectID: cfg.Effect, theme: cfg.Theme,
		pending:    make(map[chan workerAck]chan ControlReply),
		fatalOnAck: make(map[chan workerAck]bool),
	}
	o.worker = newEffectWorker(initial, cfg.Effect, cfg.Theme, o.Wake)
	return o, nil
}

func findFont(explicit string) (string, error) {
	if explicit != "" {
		fi, err := os.Stat(explicit)
		if err != nil {
			return "", err
		}
		if !fi.Mode().IsRegular() {
			return "", fmt.Errorf("wayland: font %q is not a regular file", explicit)
		}
		return explicit, nil
	}
	paths := []string{
		"/usr/share/fonts/TTF/JetBrainsMonoNerdFont-Regular.ttf",
		"/usr/share/fonts/TTF/JetBrainsMono-Regular.ttf",
		"/usr/share/fonts/noto/NotoSansMono-Regular.ttf",
	}
	for _, path := range paths {
		fi, err := os.Stat(path)
		if err == nil && fi.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", fmt.Errorf("wayland: no supported font found; tried %v", paths)
}

func (o *Owner) Wake() {
	if o == nil {
		return
	}
	o.wakeMu.Lock()
	defer o.wakeMu.Unlock()
	if o.wakeWrite < 0 {
		return
	}
	_, _ = unix.Write(o.wakeWrite, []byte{1})
}

func (o *Owner) Submit(req ControlRequest) error {
	if o == nil {
		return ErrOwnerClosed
	}
	select {
	case <-o.done:
		return ErrOwnerClosed
	default:
	}
	select {
	case <-o.done:
		return ErrOwnerClosed
	case o.controls <- req:
		o.Wake()
		return nil
	default:
		return ErrOwnerBusy
	}
}

func (o *Owner) Done() <-chan struct{} { return o.done }

func (o *Owner) Close() { o.finish() }

func (o *Owner) finish() {
	o.finishOnce.Do(func() {
		close(o.done)
		if o.worker != nil {
			o.worker.close()
		}
		o.wakeMu.Lock()
		defer o.wakeMu.Unlock()
		if o.wakeRead >= 0 {
			_ = unix.Close(o.wakeRead)
			o.wakeRead = -1
		}
		if o.wakeWrite >= 0 {
			_ = unix.Close(o.wakeWrite)
			o.wakeWrite = -1
		}
	})
}

func (o *Owner) Run(ctx context.Context, ready func()) (err error) {
	select {
	case <-o.done:
		return ErrOwnerClosed
	default:
	}
	if !o.started.CompareAndSwap(false, true) {
		return errors.New("wayland: owner can run only once")
	}
	defer o.finish()
	defer func() {
		if cleanupErr := o.cleanup(); err == nil && cleanupErr != nil {
			err = HandleDisplayLost(cleanupErr)
		}
	}()
	if err = o.connect(); err != nil {
		return err
	}
	if err = o.setupSurface(); err != nil {
		return err
	}
	return o.loop(ctx, ready)
}

func (o *Owner) connect() error {
	display, err := client.Connect("")
	if err != nil {
		return fmt.Errorf("wayland: connect: %w", err)
	}
	o.display = display
	o.ctx = display.Context()
	display.SetErrorHandler(func(e client.DisplayErrorEvent) {
		var objectID uint32
		if e.ObjectId != nil {
			objectID = e.ObjectId.ID()
		}
		o.fatal = fmt.Errorf("wl_display.error object=%d code=%d: %s", objectID, e.Code, e.Message)
	})
	registry, err := display.GetRegistry()
	if err != nil {
		return fmt.Errorf("wayland: get registry: %w", err)
	}
	o.registry = registry
	registry.SetGlobalHandler(func(e client.RegistryGlobalEvent) {
		if o.bindErr == nil {
			o.bindErr = o.bindGlobal(e)
		}
	})
	registry.SetGlobalRemoveHandler(func(e client.RegistryGlobalRemoveEvent) {
		if selected, ok := o.outputs[e.Name]; ok {
			if e.Name == o.selectedGlobal || selected.name == o.cfg.Output {
				o.fatal = fmt.Errorf("wayland: output %s disconnected", o.cfg.Output)
			}
			delete(o.outputs, e.Name)
		}
	})
	if err := display.Roundtrip(); err != nil {
		return HandleDisplayLost(err)
	}
	if o.bindErr != nil {
		return o.bindErr
	}
	if err := display.Roundtrip(); err != nil {
		return HandleDisplayLost(err)
	}
	if o.bindErr != nil {
		return o.bindErr
	}
	if o.fatal != nil {
		return HandleDisplayLost(o.fatal)
	}
	if o.compositor == nil || o.shm == nil || o.layerShell == nil || o.viewporter == nil || o.scaleManager == nil {
		return fmt.Errorf("wayland: compositor missing required globals")
	}
	if !o.argb {
		return errors.New("wayland: compositor does not support wl_shm ARGB8888")
	}
	var matches []*outputBinding
	for _, output := range o.outputs {
		if output.name == o.cfg.Output {
			matches = append(matches, output)
		}
	}
	if len(matches) != 1 {
		return fmt.Errorf("wayland: output %q matched %d outputs", o.cfg.Output, len(matches))
	}
	o.selected = matches[0]
	o.selectedGlobal = o.selected.global
	o.scale120 = o.selected.scale120
	if o.scale120 <= 0 {
		o.scale120 = 120
	}
	return nil
}

func (o *Owner) bindGlobal(e client.RegistryGlobalEvent) error {
	bind := func(proxy client.Proxy, maximum uint32) error {
		if e.Version == 0 {
			return fmt.Errorf("wayland: %s advertised version zero", e.Interface)
		}
		return o.registry.Bind(e.Name, e.Interface, min(e.Version, maximum), proxy)
	}
	switch e.Interface {
	case "wl_compositor":
		if o.compositor == nil {
			p := client.NewCompositor(o.ctx)
			if err := bind(p, maxWaylandVersion); err != nil {
				return err
			}
			o.compositor = p
		}
	case "wl_shm":
		if o.shm == nil {
			p := client.NewShm(o.ctx)
			if err := bind(p, 1); err != nil {
				return err
			}
			p.SetFormatHandler(func(e client.ShmFormatEvent) {
				if uint32(e.Format) == formatARGB8888 {
					o.argb = true
				}
			})
			o.shm = p
		}
	case "zwlr_layer_shell_v1":
		if o.layerShell == nil {
			p := layershell.NewZwlrLayerShellV1(o.ctx)
			if err := bind(p, 5); err != nil {
				return err
			}
			o.layerShell = p
		}
	case "wp_viewporter":
		if o.viewporter == nil {
			p := viewporter.NewWpViewporter(o.ctx)
			if err := bind(p, 1); err != nil {
				return err
			}
			o.viewporter = p
		}
	case "wp_fractional_scale_manager_v1":
		if o.scaleManager == nil {
			p := fractionalscale.NewWpFractionalScaleManagerV1(o.ctx)
			if err := bind(p, 1); err != nil {
				return err
			}
			o.scaleManager = p
		}
	case "wl_output":
		if e.Version < 4 {
			return fmt.Errorf("wayland: wl_output v4 required to select %q by name", o.cfg.Output)
		}
		if _, exists := o.outputs[e.Name]; exists {
			return nil
		}
		p := client.NewOutput(o.ctx)
		if err := bind(p, 4); err != nil {
			return err
		}
		output := &outputBinding{global: e.Name, version: min(e.Version, 4), proxy: p, scale120: 120}
		p.SetNameHandler(func(e client.OutputNameEvent) { output.name = e.Name })
		p.SetScaleHandler(func(e client.OutputScaleEvent) {
			if e.Factor <= 0 {
				o.fatal = fmt.Errorf("wayland: output %s has invalid integer scale %d", output.name, e.Factor)
				return
			}
			output.scale120 = int(e.Factor) * 120
			if output.global == o.selectedGlobal && !o.preferredScale && o.logicalWidth > 0 && o.logicalHeight > 0 {
				o.scale120 = output.scale120
				o.pendingLayout = &layoutRequest{width: o.logicalWidth, height: o.logicalHeight, scale120: o.scale120}
			}
		})
		o.outputs[e.Name] = output
	}
	return nil
}

func (o *Owner) setupSurface() error {
	surface, err := o.compositor.CreateSurface()
	if err != nil {
		return fmt.Errorf("wayland: create surface: %w", err)
	}
	o.surface = surface
	layer, err := o.layerShell.GetLayerSurface(surface, o.selected.proxy, uint32(LayerSpec.Layer), LayerSpec.Namespace)
	if err != nil {
		return fmt.Errorf("wayland: create layer surface: %w", err)
	}
	o.layer = layer
	viewport, err := o.viewporter.GetViewport(surface)
	if err != nil {
		return fmt.Errorf("wayland: create viewport: %w", err)
	}
	o.viewport = viewport
	fractional, err := o.scaleManager.GetFractionalScale(surface)
	if err != nil {
		return fmt.Errorf("wayland: get fractional scale: %w", err)
	}
	o.fractional = fractional
	fractional.SetPreferredScaleHandler(func(e fractionalscale.WpFractionalScaleV1PreferredScaleEvent) {
		if e.Scale == 0 || e.Scale > math.MaxInt32 {
			o.fatal = fmt.Errorf("wayland: invalid preferred scale %d/120", e.Scale)
			return
		}
		changed := int(e.Scale) != o.scale120 || !o.preferredScale
		o.scale120 = int(e.Scale)
		o.preferredScale = true
		if changed && o.logicalWidth > 0 && o.logicalHeight > 0 {
			o.pendingLayout = &layoutRequest{width: o.logicalWidth, height: o.logicalHeight, scale120: o.scale120}
		}
	})
	layer.SetConfigureHandler(func(e layershell.ZwlrLayerSurfaceV1ConfigureEvent) {
		if err := layer.AckConfigure(e.Serial); err != nil {
			o.fatal = fmt.Errorf("wayland: ack configure: %w", err)
			return
		}
		if e.Width > math.MaxInt32 || e.Height > math.MaxInt32 {
			o.fatal = fmt.Errorf("wayland: configured size %dx%d exceeds int32", e.Width, e.Height)
			return
		}
		o.configure = &pendingConfigure{width: int(e.Width), height: int(e.Height)}
	})
	layer.SetClosedHandler(func(layershell.ZwlrLayerSurfaceV1ClosedEvent) {
		o.fatal = errors.New("wayland: layer surface was closed")
	})
	region, err := o.compositor.CreateRegion()
	if err != nil {
		return fmt.Errorf("wayland: create empty input region: %w", err)
	}
	if err := surface.SetInputRegion(region); err != nil {
		_ = region.Destroy()
		return fmt.Errorf("wayland: set empty input region: %w", err)
	}
	if err := region.Destroy(); err != nil {
		return fmt.Errorf("wayland: destroy input region: %w", err)
	}
	if err := layer.SetSize(uint32(LayerSpec.Width), uint32(LayerSpec.Height)); err != nil {
		return err
	}
	if err := layer.SetAnchor(LayerSpec.Anchor); err != nil {
		return err
	}
	if err := layer.SetExclusiveZone(LayerSpec.ExclusiveZone); err != nil {
		return err
	}
	if err := layer.SetKeyboardInteractivity(uint32(LayerSpec.Keyboard)); err != nil {
		return err
	}
	if err := surface.Commit(); err != nil {
		return fmt.Errorf("wayland: initial surface commit: %w", err)
	}
	return nil
}

func (o *Owner) loop(ctx context.Context, ready func()) error {
	deadline := time.Now().Add(startupTimeout)
	for {
		if ctx.Err() != nil || o.stopping {
			return nil
		}
		if o.fatal != nil {
			return HandleDisplayLost(o.fatal)
		}
		if err := o.waitAndDispatch(deadline); err != nil {
			return err
		}
		if ctx.Err() != nil || o.stopping {
			return nil
		}
		if o.fatal != nil {
			return HandleDisplayLost(o.fatal)
		}
		if err := o.drainControls(); err != nil {
			return err
		}
		if o.stopping {
			return nil
		}
		if err := o.drainWorker(); err != nil {
			return err
		}
		if o.released {
			released := o.released
			o.released = false
			o.collectRetired()
			if ShouldRetryPaint(released, o.pendingPaint, o.configured && o.pendingLayout == nil) {
				if err := o.paintLatest(); err != nil {
					return err
				}
			}
		}
		if err := o.applyPendingLayout(); err != nil {
			return err
		}
		if !o.ready && time.Now().After(deadline) {
			return errors.New("wayland: timed out waiting for first configured frame")
		}
		if o.ready && ready != nil {
			ready()
			ready = nil
		}
	}
}

func (o *Owner) waitAndDispatch(deadline time.Time) error {
	waylandFD := -1
	if err := o.ctx.ControlFD(func(fd int) error { waylandFD = fd; return nil }); err != nil {
		return HandleDisplayLost(err)
	}
	timeout := -1
	if !o.ready {
		remaining := time.Until(deadline)
		timeout = int(remaining / time.Millisecond)
		if timeout < 0 {
			timeout = 0
		}
	}
	fds := []unix.PollFd{{Fd: int32(waylandFD), Events: unix.POLLIN}, {Fd: int32(o.wakeRead), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, timeout)
	if errors.Is(err, unix.EINTR) {
		return nil
	}
	if err != nil {
		o.displayLost = true
		return HandleDisplayLost(err)
	}
	if n == 0 {
		return nil
	}
	if fds[1].Revents&unix.POLLIN != 0 {
		var buf [256]byte
		for {
			n, err := unix.Read(o.wakeRead, buf[:])
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
				break
			}
			if err != nil {
				break
			}
			if n == 0 {
				break
			}
		}
	}
	if fds[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
		o.displayLost = true
		return HandleDisplayLost(errors.New("Wayland display fd closed"))
	}
	if fds[0].Revents&unix.POLLIN != 0 {
		if err := o.ctx.Dispatch(); err != nil {
			o.displayLost = true
			return HandleDisplayLost(err)
		}
	}
	return nil
}

func (o *Owner) drainControls() error {
	for {
		select {
		case req := <-o.controls:
			if err := o.handleControl(req); err != nil {
				return err
			}
			if o.stopping {
				return nil
			}
		default:
			return nil
		}
	}
}

func (o *Owner) handleControl(req ControlRequest) error {
	if req.Reply == nil {
		req.Reply = make(chan ControlReply, 1)
	}
	switch req.Op {
	case ControlStatus:
		o.reply(req.Reply, ControlReply{Playing: !o.paused, ID: o.effectID, Theme: o.theme})
	case ControlPause, ControlChange, ControlReset:
		workerReply := make(chan workerAck, 1)
		workerReq := workerRequest{reply: workerReply}
		switch req.Op {
		case ControlPause:
			workerReq.op, workerReq.paused = workerPause, req.Paused
		case ControlChange:
			workerReq.op, workerReq.id, workerReq.theme, workerReq.file = workerChange, req.ID, req.Theme, req.File
		case ControlReset:
			workerReq.op = workerReset
		}
		if !o.worker.submit(workerReq) {
			o.reply(req.Reply, ControlReply{Err: ErrOwnerBusy})
			return nil
		}
		if req.Op == ControlPause && req.Paused {
			o.paused = true
			if err := o.armFrameOnly(); err != nil {
				return err
			}
		}
		o.pending[workerReply] = req.Reply
	case ControlStop:
		o.reply(req.Reply, ControlReply{})
		o.stopping = true
	default:
		o.reply(req.Reply, ControlReply{Err: errors.New("wayland: unknown control request")})
	}
	return nil
}

func (o *Owner) drainWorker() error {
	for {
		select {
		case frame := <-o.worker.frames:
			if frame.err != nil {
				return fmt.Errorf("wayland: effect frame: %w", frame.err)
			}
			if frame.grid == nil {
				continue
			}
			o.latest, o.hasLatest = frame, true
			o.pendingPaint = true
			o.effectID, o.theme = frame.id, frame.theme
			if o.configured && o.pendingLayout == nil {
				if err := o.paintLatest(); err != nil {
					return err
				}
			}
		case ack := <-o.worker.acks:
			if ack.op == workerTick {
				o.tickPending = false
			}
			if ack.reply == nil {
				continue
			}
			reply, tracked := o.pending[ack.reply]
			if !tracked {
				continue
			}
			delete(o.pending, ack.reply)
			if o.fatalOnAck[ack.reply] {
				delete(o.fatalOnAck, ack.reply)
				if ack.err != nil {
					return fmt.Errorf("wayland: resize effect: %w", ack.err)
				}
				continue
			}
			if ack.err == nil {
				switch ack.op {
				case workerPause:
					o.paused = ack.paused
					if err := o.armFrameOnly(); err != nil {
						return err
					}
				case workerChange:
					o.effectID, o.theme = ack.id, ack.theme
				}
			}
			if reply != nil {
				o.reply(reply, ControlReply{Playing: !o.paused, ID: o.effectID, Theme: o.theme, Err: ack.err})
			}
		default:
			return nil
		}
	}
}

func (o *Owner) reply(ch chan ControlReply, reply ControlReply) {
	select {
	case ch <- reply:
	default:
	}
}

func (o *Owner) applyPendingLayout() error {
	if o.configure != nil {
		o.pendingLayout = &layoutRequest{width: o.configure.width, height: o.configure.height, scale120: o.scale120}
		o.configure = nil
	}
	if !o.preferredScale || o.pendingLayout == nil {
		return nil
	}
	if len(o.retired) != 0 {
		return nil
	}
	layout := *o.pendingLayout
	layout.scale120 = o.scale120
	if layout.width <= 0 || layout.height <= 0 || layout.scale120 <= 0 {
		return fmt.Errorf("wayland: invalid configured layout %dx%d at %d/120", layout.width, layout.height, layout.scale120)
	}
	pixelSize, err := PixelSize(12, layout.scale120)
	if err != nil {
		return err
	}
	if err := o.rasterizer.SetPixelSize(pixelSize); err != nil {
		return err
	}
	cellW, cellH := o.rasterizer.CellSize()
	geometry, err := ConfiguredGrid(layout.width, layout.height, layout.scale120, cellW, cellH)
	if err != nil {
		return err
	}
	if err := o.viewport.SetDestination(int32(layout.width), int32(layout.height)); err != nil {
		return fmt.Errorf("wayland: set viewport destination: %w", err)
	}
	if err := o.surface.Commit(); err != nil {
		return fmt.Errorf("wayland: commit configured surface: %w", err)
	}
	old := o.geometry
	geometryChanged := !o.configured || geometry.PixelWidth != old.PixelWidth || geometry.PixelHeight != old.PixelHeight
	gridChanged := !o.configured || geometry.Cols != old.Cols || geometry.Rows != old.Rows
	if geometryChanged {
		generation, err := newBufferGeneration(o, o.shm, geometry.PixelWidth, geometry.PixelHeight)
		if err != nil {
			return err
		}
		if o.current != nil {
			o.current.retired = true
			if o.current.inUse() {
				o.retired = append(o.retired, o.current)
			} else if err := o.current.destroy(); err != nil {
				_ = generation.destroy()
				return err
			}
		}
		o.current = generation
	}
	o.geometry, o.logicalWidth, o.logicalHeight = geometry, layout.width, layout.height
	o.scale120 = layout.scale120
	o.configured = true
	o.pendingLayout = nil
	if o.current != nil {
		o.current.resetHistory()
	}
	if gridChanged {
		ack := make(chan workerAck, 1)
		if !o.worker.submit(workerRequest{op: workerResize, cols: geometry.Cols, rows: geometry.Rows, reply: ack}) {
			return ErrOwnerBusy
		}
		o.pending[ack] = nil
		o.fatalOnAck[ack] = true
	}
	if o.hasLatest {
		o.pendingPaint = true
		return o.paintLatest()
	}
	return nil
}

func (o *Owner) paintLatest() error {
	if !o.configured || !o.hasLatest || o.current == nil || o.latest.grid == nil ||
		!gridMatchesGeometry(o.latest.grid.Cols, o.latest.grid.Rows, o.geometry) {
		return nil
	}
	slot := o.current.freeSlot()
	if slot < 0 {
		return nil
	}
	if err := o.rasterizer.DrawChanged(o.latest.grid, o.current.lastGrid[slot], o.current.pixels(slot), o.current.width, o.current.height, o.current.stride); err != nil {
		return err
	}
	o.current.lastGrid[slot] = o.latest.grid
	if frameCallbackActionFor(o.paused, o.frameCallback != nil) == requestFrameCallback {
		cb, err := o.surface.Frame()
		if err != nil {
			return fmt.Errorf("wayland: request frame callback: %w", err)
		}
		o.frameCallback = cb
		cb.SetDoneHandler(func(client.CallbackDoneEvent) {
			_ = cb.Destroy()
			o.frameCallback = nil
			o.onFrame(time.Now())
		})
	}
	if err := o.surface.Attach(o.current.buffers[slot], 0, 0); err != nil {
		return fmt.Errorf("wayland: attach buffer: %w", err)
	}
	if err := o.surface.Damage(0, 0, int32(o.logicalWidth), int32(o.logicalHeight)); err != nil {
		return fmt.Errorf("wayland: damage surface: %w", err)
	}
	o.current.busy[slot] = true
	if err := o.surface.Commit(); err != nil {
		o.current.busy[slot] = false
		return fmt.Errorf("wayland: commit frame: %w", err)
	}
	if !o.ready {
		o.ready = true
	}
	o.pendingPaint = false
	return nil
}

func (o *Owner) onFrame(now time.Time) {
	if o.paused || o.stopping {
		return
	}
	if !o.tickPending && ShouldAdvance(o.lastTick, now) {
		if o.worker.submit(workerRequest{op: workerTick}) {
			o.tickPending = true
			o.lastTick = now
			return
		}
	}
	if !o.tickPending {
		if err := o.armFrameOnly(); err != nil {
			o.fatal = err
		}
	}
}

func (o *Owner) armFrameOnly() error {
	switch frameCallbackActionFor(o.paused, o.frameCallback != nil) {
	case cancelFrameCallback:
		_ = o.frameCallback.Destroy()
		o.frameCallback = nil
		return nil
	case keepFrameCallback:
		return nil
	}
	if o.surface == nil {
		return nil
	}
	cb, err := o.surface.Frame()
	if err != nil {
		return fmt.Errorf("wayland: request frame callback: %w", err)
	}
	o.frameCallback = cb
	cb.SetDoneHandler(func(client.CallbackDoneEvent) {
		_ = cb.Destroy()
		o.frameCallback = nil
		o.onFrame(time.Now())
	})
	if err := o.surface.Commit(); err != nil {
		return fmt.Errorf("wayland: commit frame callback: %w", err)
	}
	return nil
}

func (o *Owner) collectRetired() {
	retained := o.retired[:0]
	for _, generation := range o.retired {
		if generation.inUse() {
			retained = append(retained, generation)
		} else {
			_ = generation.destroy()
		}
	}
	o.retired = retained
}

func (o *Owner) cleanup() error {
	if o.display == nil {
		return nil
	}
	var cleanupErr error
	if o.surface != nil && !o.displayLost && o.ctx != nil {
		if o.ready {
			if err := o.surface.Attach(nil, 0, 0); err != nil {
				cleanupErr = err
			} else if err := o.surface.Commit(); err != nil {
				cleanupErr = err
			} else if err := o.display.Roundtrip(); err != nil {
				cleanupErr = err
			}
		}
	}
	if o.frameCallback != nil {
		_ = o.frameCallback.Destroy()
		o.frameCallback = nil
	}
	if o.current != nil {
		if err := o.current.destroy(); cleanupErr == nil {
			cleanupErr = err
		}
		o.current = nil
	}
	for _, generation := range o.retired {
		if err := generation.destroy(); cleanupErr == nil {
			cleanupErr = err
		}
	}
	o.retired = nil
	if o.layer != nil {
		_ = o.layer.Destroy()
	}
	if o.viewport != nil {
		_ = o.viewport.Destroy()
	}
	if o.fractional != nil {
		_ = o.fractional.Destroy()
	}
	if o.surface != nil {
		_ = o.surface.Destroy()
	}
	for _, output := range o.outputs {
		if output.version >= 3 {
			_ = output.proxy.Release()
		}
	}
	if o.layerShell != nil {
		_ = o.layerShell.Destroy()
	}
	if o.viewporter != nil {
		_ = o.viewporter.Destroy()
	}
	if o.scaleManager != nil {
		_ = o.scaleManager.Destroy()
	}
	if o.compositor != nil {
		_ = o.compositor.Destroy()
	}
	if o.registry != nil {
		_ = o.registry.Destroy()
	}
	_ = o.display.Destroy()
	if err := o.ctx.Close(); cleanupErr == nil {
		cleanupErr = err
	}
	o.display = nil
	return cleanupErr
}
