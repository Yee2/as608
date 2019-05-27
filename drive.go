package as608

import (
	"bytes"
	"github.com/pkg/errors"
	"github.com/tarm/serial"
	"io"
	"sync"
	"time"
)

type PackType byte

const (
	PackType_Command PackType = 0x01
	PackType_Data    PackType = 0x02
	PackType_End     PackType = 0x08
)

func NewPacket() *Packet {
	return &Packet{Address: [...]byte{0xff, 0xff, 0xff, 0xff}, Type: PackType_Command}
}
func NewPacketWithCommand(c Command) *Packet {
	return &Packet{Address: [...]byte{0xff, 0xff, 0xff, 0xff}, Type: PackType_Command, Data: []byte{byte(c)}}
}

type Packet struct {
	Address [4]byte
	Type    PackType
	Data    []byte
}

func (p *Packet) Bytes() []byte {
	buffer := bytes.NewBuffer([]byte{0xEF, 0x01})
	// 写入地址
	buffer.Write(p.Address[:])
	check := 0
	// 写入包类型
	buffer.WriteByte(byte(p.Type))
	check += int(byte(p.Type))

	// 写入长度
	length := uint16(len(p.Data)) + 2
	buffer.WriteByte(byte(length >> 8))
	buffer.WriteByte(byte(length >> 0))
	check += int(byte(length >> 8))
	check += int(byte(length >> 0))

	// 写入数据
	buffer.Write(p.Data)
	for _, b := range p.Data {
		check += int(b)
	}
	buffer.WriteByte(byte(check >> 8))
	buffer.WriteByte(byte(check >> 0))
	return buffer.Bytes()
}

type Device struct {
	io.ReadWriter
	Chunk
	rw        *sync.RWMutex
	listeners []chan struct {
		p *Packet
		e error
	}
	sleep chan struct{}
}

func Open(name string, baud int) (*Device, error) {
	s, err := serial.OpenPort(&serial.Config{Name: name, Baud: baud})
	if err != nil {
		return nil, errors.Wrap(err, "Initialization of the device failed")
	}
	return &Device{ReadWriter: s, Chunk: Chunk_64, rw: &sync.RWMutex{}}, nil
}

// 接收一个数据包，超时返回错误
func (d *Device) ReceiveTimeout(duration time.Duration) (*Packet, error) {
	if d.listeners == nil {
		d.daemon()
	}
	ch := make(chan struct {
		p *Packet
		e error
	})
	d.rw.RLock()
	d.listeners = append(d.listeners, ch)
	d.rw.Unlock()
	d.wake()
	select {
	case <-time.Tick(duration):
		return nil, errors.Errorf("timeout")
	case s := <-ch:
		return s.p, s.e
	}
}

// 接收一个数据包
func (d *Device) Receive() (*Packet, error) {
	if d.listeners == nil {
		d.daemon()
	}
	ch := make(chan struct {
		p *Packet
		e error
	})
	d.rw.RLock()
	d.listeners = append(d.listeners, ch)
	d.rw.Unlock()
	d.wake()
	s, ok := <-ch
	if !ok {
		return nil, errors.Errorf("chan closed")
	}
	return s.p, s.e
}

func (d *Device) wake() {
	if d.sleep == nil {
		return
	}
	timeout := time.Tick(20 * time.Millisecond)
	select {
	case <-timeout:
	case d.sleep <- struct{}{}:
	}
}
func (d *Device) daemon() {
	d.listeners = make([]chan struct {
		p *Packet
		e error
	}, 0)
	sleep := make(chan struct{})
	for {
		p, err := d.receive()
		if len(d.listeners) == 0 {
			d.sleep = sleep
			<-d.sleep
			d.sleep = nil
		}
		for _, listener := range d.listeners {
			listener <- struct {
				p *Packet
				e error
			}{p: p, e: err}
			close(listener)
		}
		d.rw.RLock()
		d.listeners = d.listeners[:0]
		d.rw.RUnlock()
	}
}

// 发送指令给指纹模块
func (d *Device) Send(packet *Packet) error {
	d.rw.Lock()
	defer d.rw.Unlock()
	Chunk := int(d.Chunk)
	if len(packet.Data) > Chunk && Chunk != 0 {
		subPack := NewPacket()
		subPack.Type = PackType_Data
		for i := 0; i < len(packet.Data)/Chunk; i++ {
			if (i+1)*Chunk > len(packet.Data) {
				subPack.Type = PackType_Data
				subPack.Data = packet.Data[i*Chunk:]
			} else {
				subPack.Data = packet.Data[i*Chunk : (i+1)*Chunk]
			}
			_, e := d.Write(subPack.Bytes())
			if e != nil {
				return e
			}
		}
		return nil
	}
	_, e := d.Write(packet.Bytes())
	return e
}
func (d *Device) receive() (*Packet, error) {
	p, err := d.fragment()
	if err != nil {
		return p, err
	}
	if p.Type == PackType_Data {
		for {
			p2, err := d.fragment()
			if err != nil {
				return p, err
			}
			p.Data = append(p.Data, p2.Data...)
			if p2.Type == PackType_End {
				break
			}
		}
	}
	return p, err
}
func (d *Device) fragment() (*Packet, error) {
	// 每个包最小长度 11 byte

	// 读取头部信息
	header, n := make([]byte, 9), 0
	for {
		i, err := d.Read(header[n:])
		n += i
		if n == 9 {
			break
		}
		if err != nil {
			return nil, errors.Wrap(err, "failed to read data")
		}
	}
	if header[0] != 0xEF || header[1] != 0x01 {
		return nil, errors.New("invalid data")
	}
	p := &Packet{}
	copy(p.Address[:], header[2:6])
	p.Type = PackType(header[6])
	length := (int(header[7]) << 8) + int(header[8])
	data, n := make([]byte, length), 0
	for {
		i, err := d.Read(data[n:])
		n += i
		if n >= length {
			break
		}
		if err != nil {
			return nil, errors.Wrap(err, "failed to read data")
		}
	}

	if n < 2 {
		return nil, errors.New("accept packet error, packet length is 0")
	}
	sum := int(header[6]) + int(header[7]) + int(header[8])
	for i := range data[:len(data)-2] {
		sum += int(data[i])
	}
	if byte(sum>>8) != data[len(data)-2] || byte(sum) != data[len(data)-1] {
		return nil, errors.New("verification failed")
	}
	p.Data = data[:length-2]
	return p, nil
}
