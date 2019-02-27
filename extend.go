package as608

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/pkg/errors"
	"image"
	"image/png"
	"log"
	"os"
	"time"
)

type Code uint8
type Args uint16

const (
	Args_Default           Args = 0
	Args_LightOff          Args = 1 << 0 // 采图背光灯控制位，0-LED 长亮，1-LED 获取图像成功后灭
	Args_Pretreatment      Args = 1 << 1 // 采图预处理控制位，0-关闭预处理，1-打开预处理；
	Args_NotReturning      Args = 1 << 2 // 注册过程中，是否要求模块在关键步骤，返回当前状态，0-要求返回，1-不要求返回；
	Args_Over              Args = 1 << 3 // 可以覆盖
	Args_Unique            Args = 1 << 4 // 禁止重复注册
	Args_AllowKeepPressing Args = 1 << 5 // 注册时，多次指纹采集过程中，是否要求手指离开才能进入下一次指纹图像采集， 0-要求离开；1-不要求离开；
)

func (u16 Args) Bytes() []byte {
	return []byte{byte(u16 >> 8), byte(u16)}
}

const (
	//00H：表示指令执行完毕或 OK；
	Code_OK Code = iota
	//01H：表示数据包接收错误；
	Code_ReceiveFailure
	//02H：表示传感器上没有手指；
	Code_NotFinger
	//03H：表示录入指纹图像失败；
	Code_AddFailed
	//04H：表示指纹图像太干、太淡而生不成特征；
	Code_TooDry
	//05H：表示指纹图像太湿、太糊而生不成特征；
	Code_TooMoist
	//06H：表示指纹图像太乱而生不成特征；
	Code_TooMessy
	//07H：表示指纹图像正常，但特征点太少（或面积太小）而生不成特征；
	Code_TooFewFeatures
	//08H：表示指纹不匹配；
	Code_Mismatch
	//09H：表示没搜索到指纹；
	Code_NotFound
	//0aH：表示特征合并失败；
	Code_MergeFailed
	//0bH：表示访问指纹库时地址序号超出指纹库范围；
	Code_OutofRange
	//0cH：表示从指纹库读模板出错或无效；
	Code_InvalidTemplate
	//0dH：表示上传特征失败；
	Code_UpdateFeatureFailed
	//0eH：表示模块不能接受后续数据包；
	Code_RefusePacket
	//0fH：表示上传图像失败；
	Code_UpdateImageFailed
	//10H：表示删除模板失败；
	Code_DeleteTemplateFailed
	//11H：表示清空指纹库失败；
	Code_FlushFailed
	//13H：表示口令不正确；
	Code_InvalidPassword
	//15H：表示缓冲区内没有有效原始图而生不成图像；
	_
	//18H：表示读写 FLASH 出错；
	Code_IOError
	//19H：未定义错误；
	Code_Undefined
	//1aH：无效寄存器号；
	Code_InvalidRegisterNumber
	//1bH：寄存器设定内容错误号；
	Code_RegisterNumberError
	//1cH：记事本页码指定错误；
	//1dH：端口操作失败；
	//1eH：自动注册（enroll）失败；
	//1fH：指纹库满
	//29. 20—efH：Reserved。
)

type Command byte

const (
	Command_UpImage   Command = 0x0A
	Command_DownImage Command = 0x0b

	Command_Cancel           Command = 0x30
	Command_Sleep            Command = 0x33
	Command_ValidTempleteNum Command = 0x1d
	Command_Empty            Command = 0x0d
	Command_ReadIndexTable   Command = 0x1f
)

type Chunk int

const (
	Chunk_64  Chunk = 64
	Chunk_128 Chunk = 128
	Chunk_256 Chunk = 256
)

//从传感器上读入图像存于图像缓冲区
func (d *Device) GetImage() error {
	for {
		err := d.Send(NewPacketWithCommand(0x01))
		if err != nil {
			log.Fatalln(err)
		}
		p, err := d.Receive()
		if err != nil {
			log.Fatalln(err)
		}
		switch p.Data[0] {
		case 0x00:
			// 录入成功
			return nil
		case 0x01:
			return errors.New("error with code (0x01)")
		case 0x02:
			time.Sleep(time.Second)
			//fmt.Println("请将手指放在传感器上面")
		case 0x03:
			return errors.New("read failure")
		default:
			return errors.New("unknown response")
		}
	}
}
func (d *Device) UpImage(filename string) error {
	if err := d.GetImage(); err != nil {
		return err
	}
	if err := d.Send(NewPacketWithCommand(Command_UpImage)); err != nil {
		return errors.Wrap(err, "UpImage")
	}
	p, err := d.Receive()
	if err != nil {
		return errors.Wrap(err, "UpImage")
	}
	switch p.Data[0] {
	case 0x00:
	case 0x01:
		return errors.Errorf("error with code (0x%02x)", 0x01)
	case 0x0f:
		return errors.Errorf("error with code (0x%02x)", 0x0f)
	default:
		return errors.Errorf("unknown code:0x%02x", p.Data[0])
	}
	p, err = d.Receive()
	if err != nil {
		return errors.Wrap(err, "receive image fail")
	}

	img := image.NewGray16(image.Rect(0, 0, 256, 288))
	raw := make([]uint8, len(p.Data)*2)
	for i, gray := range p.Data {
		raw[i*2] = uint8(gray & 0xf0)
		raw[i*2+1] = uint8(gray&0x0f) << 4
	}
	// FIXME: 这里可能出现超出切片范围的错误
	for i := range raw {
		img.Pix[2*i] = raw[i]
		img.Pix[2*i+1] = 0xff
	}
	//copy(img.Pix, []uint8(p.Data))
	out, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalln(err)
	}
	defer func() {
		_ = out.Close()
	}()
	err = png.Encode(out, img)
	if err != nil {
		return err
	}
	return nil
}
func (d *Device) Search() (id, score int, e error) {
	packet := NewPacket()
	packet.Data = []byte{0x32, 0x03, 0xff, 0xff, 0x00, 0x00 << 1}
	_, err := d.Write(packet.bytes())
	if err != nil {
		return 0, 0, errors.Wrap(err, "write error")
	}
	for {
		resp, err := d.Receive()
		if err != nil {
			return 0, 0, errors.Wrap(err, "read error")
		}
		switch resp.Data[1] {
		case 0x00: //指纹合法性检测,请将手指放在上面
			if resp.Data[0] != 0x00 {
				return 0, 0, errors.Wrapf(err, "error with code (0x%02x)", resp.Data[0])
			}
		case 0x01: //获取图像，成功，正在搜索数据库
			if resp.Data[0] != 0x00 {
				return 0, 0, errors.Wrapf(err, "error with code (0x%02x)", resp.Data[0])
			}
		case 0x05: //已注册指纹比对
			id = (int(resp.Data[2]) << 8) + int(resp.Data[3])
			score = (int(resp.Data[4]) << 8) + int(resp.Data[5])
			return id, score, nil
		default:
			return 0, 0, errors.Wrapf(err, "unknown error: 0x%02x", resp.Data[1])
		}
	}
}

func (d *Device) Empty() error {
	e := d.Send(NewPacketWithCommand(Command_Empty))
	if e != nil {
		return e
	}
	p, e := d.Receive()
	if e != nil {
		return e
	}
	switch p.Data[0] {
	case 0x00:
		return nil
	default:
		return errors.Errorf("read fail with code 0x%02x", p.Data[0])
	}
}
func (d *Device) ReadIndexTable() ([]int, error) {
	arrays := make([]int, 0)
	for page := 0; page < 2; page++ {
		p := NewPacket()
		p.Data = []byte{0x1f, byte(page)}
		if err := d.Send(p); err != nil {
			return nil, err
		}
		p, err := d.Receive()
		if err != nil {
			return nil, err
		}
		if p.Data[0] != 0x00 {
			return nil, errors.New("read fail")
		}
		for i, b := range p.Data[1:] {
			for j := 0; j < 8; j++ {
				if (b & (1 << uint(j))) == (1 << uint(j)) {
					arrays = append(arrays, (page*(0xff+1))+8*i+j)
				}
			}
		}
	}
	return arrays, nil
}

type Inf struct {
	SSR           uint16
	SensorType    uint16
	DataBaseSize  uint16
	SecurLevel    uint16
	DeviceAddress [4]byte
	// CFG_PktSize 0:32 Bytes 1:64 Bytes 2:128 Bytes 3:256 Bytes
	CFG_PktSize         uint16
	CFG_BaudRate        uint16
	CFG_VID             uint16
	CFG_PID             uint16
	_                   uint64
	ProductSN           [8]byte
	SoftwareVersion     [8]byte
	Manufacturer        [8]byte
	SensorName          [8]byte
	PassWord            [4]byte
	JtagLockFlag        [4]byte
	SensorInitEntry     uint16
	SensorGetImageEntry uint16
	Resevd              [54]uint8
	ParaTableFlag       uint16
}

func (d *Device) Information() (*Inf, error) {
	err := d.Send(NewPacketWithCommand(0x16))
	if err != nil {
		return nil, err
	}
	p, err := d.Receive()
	if err != nil {
		return nil, err
	}
	switch p.Data[0] {
	case 0x00:
	case 0x01:
		return nil, errors.Errorf("read fail with code 0x%02x", p.Data[0])
	case 0x0d:
		return nil, errors.Errorf("read fail with code 0x%02x", p.Data[0])
	default:
		return nil, errors.Errorf("unknown response: 0x%02x", p.Data[0])
	}

	p, err = d.Receive()
	if err != nil {
		return nil, err
	}
	inf := &Inf{}
	err = binary.Read(bytes.NewReader(p.Data), binary.BigEndian, inf)
	if err != nil {
		return nil, err
	}
	if inf.ParaTableFlag != 0x1234 {
		return nil, errors.New("ParaTableFlag Verification failed")
	}
	return inf, nil
}

// 读有效模板个数
func (d *Device) ValidTempleteNum() (int, error) {
	e := d.Send(NewPacketWithCommand(Command_ValidTempleteNum))
	if e != nil {
		return 0, e
	}
	p, e := d.Receive()
	if e != nil {
		return 0, e
	}
	switch p.Data[0] {
	case 0x00:
		return (int(p.Data[1]) << 8) + int(p.Data[2]), nil
	default:
		return 0, errors.Errorf("read fail with code 0x%02x", p.Data[0])
	}
}
func (d *Device) GetSN() (string, error) {
	packet := NewPacket()
	packet.Data = []byte{0x34}
	_, err := d.Write(packet.bytes())
	if err != nil {
		return "", errors.Wrap(err, "GetSN fail")
	}

	p, err := d.Receive()
	if err != nil {
		return "", errors.Wrap(err, "GetSN fail")
	}
	return fmt.Sprintf("%X", p.Data), nil
}
