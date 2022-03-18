package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/go-redis/redis"
	"gopkg.in/ini.v1"
	"io"
	"os"
	"strconv"
	"time"
	"os/exec"
	"strings"
	"path/filepath"
	"log"
	"bytes"
	"io/ioutil"
	"net/http"
)

var (
	path_conf []map[string]string
	confs map[string]interface{}
	rdb *redis.Client
	file_record  map[string]map[string]int64
	count int

	WarningLogger *log.Logger
	InfoLogger    *log.Logger
	ErrorLogger   *log.Logger
)


func init() {
	conf := flags()
	config(conf)
	logconfig()
	redisrun()
	err := fileIniRead()
	if err != nil {
		ErrorLogger.Println(err)
		fmt.Println(err)
		os.Exit(1)
	}
}

func main() {
	for {
		fileIniWrite_count := 0
		now := time.Now().Unix()
		text_data := []interface{}{}
		for _, v := range path_conf{
			source_path := v["source_path"]
			file_exec := exec.Command("find", source_path, "-ctime", "-1", "-type", "f", "-name", "*.log")

			tp_file_data, err := file_exec.Output()
			if err != nil {
				ErrorLogger.Println("执行命令出错：", err)
				alarmsJmc(fmt.Sprintf("执行命令出错：%v", err))
				os.Exit(1)
			}
			file_data := strings.Split(strings.TrimSuffix(string(tp_file_data), "\n"), "\n")
			if len(file_data) == 0{
				continue
			}
			date := time.Now().Format("20060102")
			//fmt.Printf("本轮查询到要同步的日志：%v \n", file_data)
			for _, f := range file_data{
				if len(f) <= 2{
					continue
				}
				target_path := filepath.Join(v["target_path"], date, v["name"], strings.Split(strings.TrimSuffix(f, "\n"), source_path)[1]) //生成目标路径
				seek := fileRecord_status(f, file_record)

				file, err := os.Open(f)
				if err != nil {
					ErrorLogger.Println("读取文件失败: ", err)
					alarmsJmc(fmt.Sprintf("读取文件失败：%v", err))
					return
				}

				// 处理业务日志框架 - 业务日志框架会将日志大小进行切割
				seekEND, _ := file.Seek(0,os.SEEK_END)
				if seekEND < seek{
					_, ok := file_record[f]
					if ok{
						fileInfo, err :=os.Stat(f)
						if err != nil {
							ErrorLogger.Println("获取文件信息出错: ", err)
							alarmsJmc(fmt.Sprintf("获取文件信息出错：%v", err))
							continue
						}
						fileModTime := fileInfo.ModTime().Unix()
						if (fileModTime - file_record[f]["Unix"]) > 30{
							WarningLogger.Println(fmt.Sprintf("日志文件：%v，发生重写行为，重新获取文件", f))
							seek = int64(0)
						}else {
							continue
						}
					}
				}

				file.Seek(seek, os.SEEK_SET)

				ctx := make([]byte, 65536)  // 定义一个切片来存储取的数据，长度就代表要取多少数据
				for { // 读取后，会自动将文件指针放在读取的文件数据后，这样才能循环读取文件数据
					n, err := file.Read(ctx) // n 代表取的数量
					if err == io.EOF { // 文件读取完后，在读取就会报EOF错误，那么就可以通过判断err，来判断文件是否读取完毕
						break
					}
					if err != nil {
						ErrorLogger.Printf("文件：%v， 读取失败 %v \n", f, err)
						alarmsJmc(fmt.Sprintf("文件：%v， 读取失败 %v", f, err))
						break
					}
					tp_data := string(ctx[:n])
					if len(tp_data) >= 1{
						dataType , err := json.Marshal(map[string]string{
							"name": v["name"],
							"target_path": target_path,
							"data": tp_data,
						})
						if err != nil {
							ErrorLogger.Println("数据处理有问题，无法转换", err)
							alarmsJmc(fmt.Sprintf("数据处理有问题，无法转换：%v", err))
							os.Exit(1)
						}
						fileIniWrite_count ++
						text_data = append(text_data, dataType)
					}
					if len(text_data) >= 1000{
						err := redisSet(text_data)
						if err != nil {
							ErrorLogger.Println("写入redis失败：", err)
							alarmsJmc(fmt.Sprintf("写入redis失败：：%v", err))
							os.Exit(1)
						}
						text_data = []interface{}{}
						time.Sleep(time.Duration(50)*time.Millisecond)
					}
				}
				se, _ := file.Seek(0,os.SEEK_CUR)
				if se == 0{
					WarningLogger.Printf("文件：%v，读取后获取到的指针为0，请注意\n", f)
					//alarmsJmc(fmt.Sprintf("文件：%v，获取到的指针为0，请注意", f))
				}else{
					file_record[f] = map[string]int64{"Unix": now, "Seek": se}
				}
				//file_record[f] = map[string]int64{"Unix": now, "Seek": se}
				if fileIniWrite_count >= 100{
					err := fileIniWrite(file_record)
					if err != nil {
						ErrorLogger.Println("写入指针文件失败", err)
						alarmsJmc(fmt.Sprintf("写入指针文件失败：%v", err))
						os.Exit(1)
					}
					text_data = []interface{}{}
				}
				file.Close()
			}
		}
		if len(text_data) >= 1{
			err := redisSet(text_data)
			if err != nil {
				ErrorLogger.Println("写入redis失败：", err)
				alarmsJmc(fmt.Sprintf("写入redis失败：：%v", err))
				os.Exit(1)
			}
			text_data = []interface{}{}
			time.Sleep(time.Duration(50)*time.Millisecond)
		}
		if fileIniWrite_count >= 1{
			WarningLogger.Printf("轮查结束，检测有文件发生变动: %v，更新指针配置文件\n", fileIniWrite_count)
			err := fileIniWrite(file_record)
			if err != nil {
				ErrorLogger.Println("写入指针文件失败", err)
				alarmsJmc(fmt.Sprintf("写入指针文件失败：：%v", err))
				os.Exit(1)
			}
		}else{
			InfoLogger.Println("轮查结束，检测没有文件发生变动，无需更新指针配置文件")
		}
		time.Sleep(time.Duration(confs["file_time"].(int))*time.Second)
		for {
			if redisllen(){
				break
			}else{
				ErrorLogger.Println("redis存储数据超过2万条，请注意sever端是否正常运行或请注意redis是否正常")
				alarmsJmc(fmt.Sprintf("redis存储数据超过2万条，请注意sever端是否正常运行或请注意redis是否正常"))
				time.Sleep(time.Duration(3)*time.Second)
			}
		}
	}
}

func redisllen() bool {
	llen := rdb.LLen("collectionLog").Val()
	if llen >= 20000 {
		return false
	}else if llen < 20000{
		return true
	}
	return false
}

func redisSet(data []interface{}) error {
	//rdb.FlushDB()
	queen := 100
	status := 0
	for {
		data_count := len(data)
		if data_count < queen {
			queen = data_count
			status = 1
		}
		if queen == 0 {
			return nil
		}
		result, err := rdb.RPush("collectionLog", data[:queen]...).Result()
		if err != nil {
			fmt.Println(err)
			return fmt.Errorf("redis Write failed")
		}
		WarningLogger.Println("写入的列表索引号：", result)
		if status == 1 {
			return nil
		}
		data = data[queen:]
		time.Sleep(time.Duration(3)*time.Millisecond)
	}
}

func fileIniWrite(da map[string]map[string]int64) error {
	now := time.Now().Unix()
	data := map[string]map[string]int64{}
	for k, v := range da {
		if (now - v["Unix"]) < confs["file_record_time"].(int64) {
			data[k] = v
		}
	}
	file_record = data
	file, err := os.Create(confs["file_path"].(string))
	defer file.Close()  // 定义失败或程序结束后关闭文件
	if err != nil {
		return fmt.Errorf("打开指针文件失败：%v", err)
	}

	dataType , err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("写入指针文件的数据处理有问题，无法转换：%v", err)
	}

	_, err = file.WriteString(fmt.Sprintf("%v", string(dataType))) // 直接写字符串
	if err != nil {
		return fmt.Errorf("写入指针文件失败: %v", err)

	}
	return nil
}

func fileIniRead() error {
	fi, err := os.Stat(confs["file_path"].(string))
	if err != nil || ! fi.Mode().IsRegular() {
		file_record = map[string]map[string]int64{}
		return nil
	}

	file, err := os.Open(confs["file_path"].(string)) // 读取文件名为1的文件，相对路径
	defer file.Close()  // 定义失败或程序结束后关闭文件
	if err != nil {
		return fmt.Errorf("读取指针配置文件失败：%v", err)
	}
	var tp_da string
	ctx := make([]byte, 1024)
	for { // 读取后，会自动将文件指针放在读取的文件数据后，这样才能循环读取文件数据
		n, err := file.Read(ctx) // n 代表取的数量
		if err == io.EOF { // 文件读取完后，在读取就会报EOF错误，那么就可以通过判断err，来判断文件是否读取完毕
			break
		}
		tp_da += string(ctx[:n])
	}
	if len(tp_da) >= 2 {
		err = json.Unmarshal([]byte(tp_da), &file_record)
		if err != nil {
			return fmt.Errorf("指针配置文件格式有误，无法转换：%v", err)
		}
	}else {
		file_record = map[string]map[string]int64{}
		return nil
	}
	return nil
}

func fileRecord_status(name string, file_record map[string]map[string]int64) int64 {
	for k, v := range file_record{
		if name == k{
			return v["Seek"]
		}
	}
	return 0
}

func flags() string {
	var conf string
	flag.StringVar(&conf, "conf", "./conf.ini", "配置文件")

	//解析命令行参数
	flag.Parse()
	//返回使用的命令行参数个数
	if flag.NFlag() == 0 {
		fmt.Println("请输入配置文件")
		os.Exit(1)
	}
	fi, err := os.Stat(conf)
	if err != nil {
		fmt.Println("获取配置文件失败", err)
		os.Exit(1)
	}
	if ! fi.Mode().IsRegular() {
		fmt.Println("输入的配置文件不存在")
		os.Exit(1)
	}
	return conf
}

func alarmsJmc(da string) bool {
	if confs["alarms_status"].(int) == 0{
		WarningLogger.Println("不开启报警，拒绝报警操作")
		return true
	}
	content, data := make(map[string]string), make(map[string]interface{})
	content["content"] = confs["alarms_jmc_prefix"].(string) + "\n" + da
	data["msgtype"] = "text"
	data["text"] = content
	b, _ := json.Marshal(data)

	resp, err := http.Post(confs["alarms_jmc_url"].(string),
		"application/json",
		bytes.NewBuffer(b))
	defer resp.Body.Close()
	if err != nil {
		ErrorLogger.Println("钉钉报警出错：", err)
		return false
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		ErrorLogger.Println("钉钉报警出错：", err)
		return false
	}
	InfoLogger.Println(string(body))
	return true
}

func logconfig()  {
	file, err := os.OpenFile(confs["log_path"].(string), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		fmt.Println("日志初始化失败，请确定日志路径正确存在：", err)
		log.Fatal("日志初始化失败，请确定日志路径正确存在：", err)
		os.Exit(1)
	}

	InfoLogger = log.New(file, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile)
	WarningLogger = log.New(file, "WARNING: ", log.Ldate|log.Ltime|log.Lshortfile)
	ErrorLogger = log.New(file, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
}

func config(conf string) {
	cfg, err := ini.Load(conf)
	if err != nil {
		fmt.Printf("配置文件格式有问题: %v", err)
		os.Exit(1)
	}
	err = json.Unmarshal([]byte(cfg.Section("log").Key("conf").String()), &path_conf)
	if err != nil {
		fmt.Println("配置文件格式有误，无法转换", err)
		os.Exit(1)
	}

	file_record_time, err := strconv.ParseInt(cfg.Section("file").Key("file_record_time").String(), 10, 64)
	if err != nil {
		fmt.Println("配置文件格式有误，无法转换", err)
		os.Exit(1)
	}

	file_time, err := strconv.Atoi(cfg.Section("file").Key("file_time").String())
	if err != nil {
		fmt.Println("配置文件格式有误，无法转换", err)
		os.Exit(1)
	}

	alarms_status, err := strconv.Atoi(cfg.Section("server").Key("alarms_status").String())
	if err != nil {
		fmt.Println("配置文件格式有误，无法转换", err)
		os.Exit(1)
	}

	confs = map[string]interface{}{
		"redis_host" : cfg.Section("redis").Key("host").String(),
		"redis_pass" : cfg.Section("redis").Key("pass").String(),
		"file_path" : cfg.Section("file").Key("file_path").String(),
		"file_record_time" : file_record_time,
		"file_time" : file_time,
		"alarms_status" : alarms_status,
		"log_path" : cfg.Section("server").Key("log_path").String(),
		"alarms_jmc_url" : cfg.Section("server").Key("alarms_jmc_url").String(),
		"alarms_jmc_prefix" : cfg.Section("server").Key("alarms_jmc_prefix").String(),
	}
}


func redisrun() {
	rdb = redis.NewClient(&redis.Options{
		//连接信息
		Network:  "tcp",                  //网络类型，tcp or unix，默认tcp
		Addr:     confs["redis_host"].(string), //主机名+冒号+端口，默认localhost:6379
		Password: confs["redis_pass"].(string),                     //密码
		DB:       10,                      // redis数据库index

		//连接池容量及闲置连接数量
		PoolSize:     1, // 连接池最大socket连接数，默认为4倍CPU数， 4 * runtime.NumCPU
		MinIdleConns: 1, //在启动阶段创建指定数量的Idle连接，并长期维持idle状态的连接数不少于指定数量；。

		//超时
		DialTimeout:  5 * time.Second, //连接建立超时时间，默认5秒。
		ReadTimeout:  3 * time.Second, //读超时，默认3秒， -1表示取消读超时
		WriteTimeout: 60 * time.Second, //写超时，默认等于读超时
		PoolTimeout:  4 * time.Second, //当所有连接都处在繁忙状态时，客户端等待可用连接的最大等待时长，默认为读超时+1秒。

		//闲置连接检查包括IdleTimeout，MaxConnAge
		IdleCheckFrequency: 600 * time.Second, //闲置连接检查的周期，默认为1分钟，-1表示不做周期性检查，只在客户端获取连接时对闲置连接进行处理。
		IdleTimeout:        5 * time.Minute,  //闲置超时，默认5分钟，-1表示取消闲置超时检查
		MaxConnAge:         0 * time.Second,  //连接存活时长，从创建开始计时，超过指定时长则关闭连接，默认为0，即不关闭存活时长较长的连接

		//命令执行失败时的重试策略
		MaxRetries:      0,                      // 命令执行失败时，最多重试多少次，默认为0即不重试
		MinRetryBackoff: 8 * time.Millisecond,   //每次计算重试间隔时间的下限，默认8毫秒，-1表示取消间隔
		MaxRetryBackoff: 512 * time.Millisecond, //每次计算重试间隔时间的上限，默认512毫秒，-1表示取消间隔

		//可自定义连接函数
		//Dialer: func() (net.Conn, error) {
		//	netDialer := &net.Dialer{
		//		Timeout:   5 * time.Second,
		//		KeepAlive: 5 * time.Minute,
		//	}
		//	return netDialer.Dial("tcp", "127.0.0.1:6379")
		//},

		//钩子函数
		OnConnect: func(conn *redis.Conn) error { //仅当客户端执行命令时需要从连接池获取连接时，如果连接池需要新建连接时则会调用此钩子函数
			WarningLogger.Printf("连接redis信息，conn=%v\n", conn)
			return nil
		},
	})
	_, err := rdb.Ping().Result()
	if err != nil {
		ErrorLogger.Println("redis连接失败", err)
		alarmsJmc(fmt.Sprintf("redis连接失败：%v", err))
		os.Exit(1)
	}
}