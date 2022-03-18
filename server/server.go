package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/go-ini/ini"
	"github.com/go-redis/redis"
	"os"
	"sync"
	"time"
	"path/filepath"
	"strconv"
	"log"
	"bytes"
	"io/ioutil"
	"net/http"
)

var (
	confs map[string]interface{}
	rdb *redis.Client
	locker sync.Mutex
	wg sync.WaitGroup
	multiprocess_count int

	WarningLogger *log.Logger
	InfoLogger    *log.Logger
	ErrorLogger   *log.Logger
)

func init() {
	conf := flags()
	config(conf)
	logconfig()
	redisrun()
}

func main() {
	for i := 0; i < multiprocess_count; i++ {
		wg.Add(1)
		go multiprocess()
		InfoLogger.Printf("处理子进程%v 启动! \n", i+1)
	}
	wg.Wait()
}

func multiprocess() {
	defer wg.Done()
	for {
		locker.Lock()
		result, err :=  redisGet()
		locker.Unlock()
		if err != nil {
			ErrorLogger.Println("获取redis数据失败：", err)
			alarmsJmc(fmt.Sprintf("获取redis数据失败：%v", err))
			continue
		}
		if len(result) == 0{
			time.Sleep(time.Duration(1)*time.Nanosecond)
			continue
		}
		var tp_da map[string]string
		err = json.Unmarshal([]byte(result[0]), &tp_da)
		if err != nil {
			ErrorLogger.Println("日志文件转换格式有误，无法转换", err)
			alarmsJmc(fmt.Sprintf("日志文件转换格式有误，无法转换：%v", err))
			continue
		}
		err = fileIniWrite(tp_da)
		if err != nil {
			ErrorLogger.Printf("写入日志文件出错：%v", err)
			alarmsJmc(fmt.Sprintf("写入日志文件出错：%v", err))
			continue
		}
	}
}

func fileIniWrite(da map[string]string) error {
	target_path := da["target_path"]
	dir, _ := filepath.Split(target_path)
	exists, err := dirExists(dir)
	if err != nil {
		return fmt.Errorf("目录：%v, 判断目录是否存在失败：%v \n", target_path, err)
	}
	if ! exists{
		err = os.MkdirAll(dir, os.ModePerm)
		if err != nil {
			return fmt.Errorf("目录：%v, 创建目录失败：%v \n", target_path, err)
		}
	}

	file, err := os.OpenFile(target_path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666) //打开文件
	defer file.Close() // 定义失败或程序结束后关闭文件
	if err != nil {
		return fmt.Errorf("打开指针文件失败：%v\n", err)
	}

	locker.Lock()
	_, err = file.WriteString(da["data"]) // 直接写字符串
	locker.Unlock()
	if err != nil {
		return fmt.Errorf("写入指针文件失败: %v\n", err)
	}
	return nil
}


func dirExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}


func redisGet() ([]string, error) {
	//rdb.FlushDB()
	llen := rdb.LLen("collectionLog").Val()
	if llen == 0 {
		return []string{}, nil
	}else if llen >= 20000{
		ErrorLogger.Println("redis存储数据超过2万条，server端消费不过来")
		alarmsJmc(fmt.Sprintf("redis存储数据超过2万条，server端消费不过来"))
	}
	result, err := rdb.LRange("collectionLog", 0, 0).Result()
	if err != nil {
		return []string{}, fmt.Errorf("获取redis数据失败: %v", err)
	}
	_, err = rdb.LPop("collectionLog").Result()
	time.Sleep(time.Duration(1)*time.Microsecond)
	if err != nil {
		return []string{}, fmt.Errorf("删除已获取的数据失败: %v", err)
	}
	return result, nil

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

func config(conf string) {
	cfg, err := ini.Load(conf)
	if err != nil {
		fmt.Printf("配置文件格式有问题: %v", err)
		os.Exit(1)
	}

	multiprocess_count, err = strconv.Atoi(cfg.Section("server").Key("multiprocess_count").String())
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
		"log_path" : cfg.Section("server").Key("log_path").String(),
		"alarms_status" : alarms_status,
		"alarms_jmc_url" : cfg.Section("server").Key("alarms_jmc_url").String(),
		"alarms_jmc_prefix" : cfg.Section("server").Key("alarms_jmc_prefix").String(),
	}
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

func redisrun() {
	rdb = redis.NewClient(&redis.Options{
		//连接信息
		Network:  "tcp",                        //网络类型，tcp or unix，默认tcp
		Addr:     confs["redis_host"].(string), //主机名+冒号+端口，默认localhost:6379
		Password: confs["redis_pass"].(string), //密码
		DB:       10,                           // redis数据库index

		//连接池容量及闲置连接数量
		PoolSize:     4, // 连接池最大socket连接数，默认为4倍CPU数， 4 * runtime.NumCPU
		MinIdleConns: 2, //在启动阶段创建指定数量的Idle连接，并长期维持idle状态的连接数不少于指定数量；。

		//超时
		DialTimeout:  60 * time.Second, //连接建立超时时间，默认5秒。
		ReadTimeout:  3 * time.Second, //读超时，默认3秒， -1表示取消读超时
		WriteTimeout: 3 * time.Second, //写超时，默认等于读超时
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