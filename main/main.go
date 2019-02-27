package main

import (
	"fmt"
	"github.com/Yee2/as608"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
	"log"
	"os"
)

var dev *as608.Device

func init() {
	var err error
	dev, err = as608.Open("COM4", 57600)
	if err != nil {
		log.Fatal(err)
	}
}
func main() {
	app := cli.NewApp()
	app.Commands = []cli.Command{
		{
			Name:   "sn",
			Usage:  "获取设备SN码",
			Action: GetSN,
		},
		{
			Name:   "enroll",
			Usage:  "自动注册指纹",
			Action: Enroll,
		},
		{
			Name:   "info",
			Usage:  "获取设备信息",
			Action: Info,
		},
		{
			Name:   "index",
			Usage:  "获取已注册模板列表",
			Action: Index,
		},
		{
			Name:   "search",
			Usage:  "搜索注册过的指纹",
			Action: Search,
		},
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Fatalln(err)
	}
}

func Search(_ *cli.Context) error {
	fmt.Println("请将手指放在传感器上面")
	id, score, err := dev.Search()
	if err != nil {
		return err
	}
	if id == 0xffff {
		fmt.Println("未找到匹配")
	} else {
		fmt.Printf("查询结果: id = %d, score = %d \n", id, score)
	}
	return nil
}
func GetSN(_ *cli.Context) error {
	sn, err := dev.GetSN()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("SN码为:%s\n", sn)
	return nil
}

func Info(_ *cli.Context) error {
	inf, e := dev.Information()
	if e != nil {
		return e
	}
	fmt.Printf("设备型号:%s\n", inf.ProductSN)
	fmt.Printf("软件版本:%s\n", inf.SoftwareVersion)
	fmt.Printf("厂家名称:%s\n", inf.Manufacturer)
	fmt.Printf("传感器名称:%s\n", inf.SensorName)
	fmt.Printf("指纹库大小:%d\n", inf.DataBaseSize)
	fmt.Printf("安全等级:%d\n", inf.SecurLevel)
	fmt.Printf("设备地址:0x%02x\n", inf.DeviceAddress)
	return nil
}
func Index(_ *cli.Context) error {
	table, err := dev.ReadIndexTable()
	if err != nil {
		return err
	}
	fmt.Println(table)
	return nil
}
func Enroll(_ *cli.Context) error {
	table, err := dev.ReadIndexTable()
	if err != nil {
		return err
	}
	p, id := 0, 0
	for _, i := range table {
		if i-p > 1 {
			id = p + 1
			goto exchange
		} else {
			p = i
		}
	}
	id = table[len(table)-1]
exchange:
	packet := as608.NewPacket()
	packet.Data = append([]byte{0x31, byte(id >> 8), byte(id), 0x02},
		(as608.Args_Default | as608.Args_Unique).Bytes()...)
	if err := dev.Send(packet); err != nil {
		return err
	}
	for {
		resp, err := dev.Receive()
		if err != nil {
			return err
		}
		fmt.Printf("Resp:%02X\n", resp.Data)
		status, step, number := resp.Data[0], resp.Data[1], resp.Data[2]
		if status == 0x26 {
			fmt.Println("超时，请重新开始注册")
			goto finally
		}
		switch step {
		case 0x00:
			if status != 0x00 {
				return errors.New(showError(resp.Data[0]))
			}
			fmt.Println("请将手指放在传感器上面")
		case 0x01:
			if status == 0x00 {
				fmt.Printf("[ %d ]采集图像成功，正在生成特征\n", number)
			} else {
				fmt.Printf("采集失败，正在重试:%s\n", showError(status))
			}
		case 0x02:
			if status == 0x00 {
				fmt.Printf("[ %d ]生成特征成功\n", number)
			} else {
				fmt.Printf("生成特征失败，正在重试:%s\n", showError(status))
			}
		case 0x03:
			fmt.Println("请移开手指，然后重新放到传感器上")
		case 0x04:
			if status == 0x00 {
				fmt.Println("合成模板成功")
			} else {
				fmt.Println("合成模板失败")
				goto finally
			}
		case 0x05:
			if status == 0x27 {
				fmt.Println("注册失败，该指纹信息已经存在数据库里")
				goto finally
			}
		case 0x06:
			if status == 0x00 {
				fmt.Println("注册指纹成功")
			} else {
				fmt.Println("注册指纹失败")
			}
			goto finally
		default:
			fmt.Printf("接收到未注册响应:0x%02x\n", resp.Data[1])
			goto finally
		}
	}
finally:
	return nil
}

func showError(code byte) string {
	switch code {
	case 0x00:
		return "注册指纹成功"
	case 0x01:
		return "注册指纹失败"
	case 0x07:
		return "生成特征失败"
	case 0x09:
		return "没搜索到指纹"
	case 0x0b:
		return "ID 号超出范围"
	case 0x17:
		return "残留指纹"
	case 0x23:
		return "指纹模板为空"
	case 0x24:
		return "指纹库为空"
	case 0x26:
		return "超时"
	case 0x27:
		return "表示指纹已存在"
	default:
		return fmt.Sprintf("未知响应代码:0x%0xX", code)
	}
}
