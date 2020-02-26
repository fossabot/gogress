package gogress

import (
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/snakeice/gogress/writer"

	"github.com/snakeice/gogress/format"
)

const (
	defaultRefreshRate = time.Second / 120
)

type Progress struct {
	ID string

	max        int64
	current    int64
	Previous   int64
	startValue int64

	RefreshRate time.Duration

	messagePrefix   string
	ShowElapsedTime bool
	ShowCounters    bool
	ShowSpeed       bool
	ShowTimeLeft    bool

	UnitsWidth   int
	TimeBoxWidth int

	Output     io.Writer
	writer     *writer.Writer
	Width      int
	ForceWidth bool
	Format     *format.ProgressFormat
	Units      format.Units

	startTime  time.Time
	changeTime time.Time

	finishOnce sync.Once
	finish     chan struct{}
	isFinish   bool

	mu   sync.Mutex
	last string
	ctx  *printerContext
}

func NewDef() *Progress {
	return New(100)
}

func New(max int) *Progress {
	return New64(int64(max))
}

func New64(max int64) *Progress {
	bar := &Progress{
		max:             max,
		finish:          make(chan struct{}),
		RefreshRate:     defaultRefreshRate,
		Format:          format.DefaultFormat,
		ShowElapsedTime: false,
		ShowCounters:    false,
		ShowSpeed:       false,
		ShowTimeLeft:    false,
		Units:           format.U_NO,
		writer:          writer.New(os.Stdout),
	}

	bar.ctx = newPrintContex(bar, max, bar.current)

	return bar
}

func (p *Progress) GetCurrent() int64 {
	return atomic.LoadInt64(&p.current)
}
func (p *Progress) GetMax() int64 {
	return atomic.LoadInt64(&p.max)
}

func (p *Progress) Set(newValue int) *Progress {
	return p.Set64(int64(newValue))
}

func (p *Progress) Set64(newValue int64) *Progress {
	atomic.StoreInt64(&p.current, newValue)
	return p
}

func (p *Progress) Inc() *Progress {
	return p.Add(1)
}

func (p *Progress) Add(incSize int) *Progress {
	return p.Add64(int64(incSize))
}

func (p *Progress) Add64(incSize int64) *Progress {
	atomic.AddInt64(&p.current, incSize)
	return p
}

func (p *Progress) SetMax(max int) *Progress {
	return p.SetMax64(int64(max))
}

func (p *Progress) SetMax64(max int64) *Progress {
	atomic.StoreInt64(&p.max, max)
	return p
}

func (p *Progress) Prefix(prefix string) *Progress {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messagePrefix = prefix
	return p
}

func (p *Progress) SetMaxWidth(maxWidth int) *Progress {
	p.Width = maxWidth
	p.ForceWidth = false
	return p
}

func (p *Progress) SetWidth(width int) *Progress {
	p.Width = width
	p.ForceWidth = true
	return p
}

func (p *Progress) String() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.last
}

func (p *Progress) GetWidth() int {
	if p.ForceWidth {
		return p.Width
	}

	width := p.Width
	termWidth, _ := p.writer.GetWidth()
	if width == 0 || termWidth <= width {
		width = termWidth
	}

	return width
}

func (p *Progress) write(total, current int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	prev := p.last
	p.ctx.
		Update(total, current).
		Feed()

	if prev == p.last {
		return
	}

	isFinish := p.isFinish
	toPrint := append([]byte(p.last), '\n')
	switch {
	case isFinish:
		return
	case p.Output != nil:
		fmt.Fprint(p.Output, toPrint)
	default:
		p.writer.Write(toPrint)
		p.writer.Flush(1)
	}
}

func (p *Progress) Finish() {
	p.finishOnce.Do(func() {
		close(p.finish)
		p.write(atomic.LoadInt64(&p.max), atomic.LoadInt64(&p.current))
		p.mu.Lock()
		defer p.mu.Unlock()
		switch {
		case p.Output != nil:
			fmt.Fprintln(p.Output)
		default:
			fmt.Println()
		}
		p.isFinish = true
	})
}

func (p *Progress) IsFinished() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.isFinish
}

func (p *Progress) FinishPrint(str string) {
	p.Finish()
	if p.Output != nil {
		fmt.Fprintln(p.Output, str)
	} else {
		fmt.Println(str)
	}
}

func (p *Progress) Update() {
	current := atomic.LoadInt64(&p.current)
	prev := atomic.LoadInt64(&p.Previous)
	max := atomic.LoadInt64(&p.max)
	if prev != current {
		p.mu.Lock()
		p.changeTime = time.Now()
		p.mu.Unlock()
		atomic.StoreInt64(&p.Previous, current)
	}
	p.write(max, current)
	if current == 0 {
		p.startTime = time.Now()
		p.startValue = 0
	} else if current >= max && !p.isFinish {
		p.Finish()
	}
}

func (p *Progress) Reset(max int) *Progress {
	return p.Reset64(int64(max))
}

func (p *Progress) Reset64(max int64) *Progress {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.isFinish {
		p.SetMax64(max).Set(0)
		atomic.StoreInt64(&p.Previous, 0)
	}
	return p
}

func (p *Progress) refresher() {
	for {
		select {
		case <-p.finish:
			return
		case <-time.After(p.RefreshRate):
			p.Update()
		}
	}
}

func (p *Progress) Start() *Progress {
	p.startTime = time.Now()
	p.startValue = atomic.LoadInt64(&p.current)
	if atomic.LoadInt64(&p.max) == 0 {
		p.ShowTimeLeft = false
	}
	p.Update()
	go p.refresher()
	return p
}
