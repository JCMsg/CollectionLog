# CollectionLog
实时本地日志同步到日志服务器工具

1. 适用于：想要源业务机日志目录的日志文件路径还原到日志服务器所同步的路径
2. 适用于：将日志文件转存到日志服务器文件中，不是转存于其他存储(数据库)



# 推荐事项

1. nginx日志同步，如需在nginx机器保留一份日志的情况下，是可以使用CollectionLog，如果在nginx机器不需要保留一份的话，推荐使用rsyslog
2. 如果对同步的日志路径没要求的话，推荐使用rsyslog
3. 如果需要对同步日志转存到其他存储中(数据库)，推荐使用rsyslog、filebeat等





# 注意事项

1. 第一次同步会同步已检测到的文件的所有数据，对于redis压力会大一点





# 安装使用

1. 需要redis做为数据缓存



### 如需编译

``` go
go build
```



### 快速使用

``` go
./server --conf ./conf.ini # 服务端启动命令
./client --conf ./conf.ini # 客户端启动命令
```





# 配置文件

### server

``` ini
[redis]
host = "127.0.0.1:6379"
# 没密码为空，推荐使用密码
pass = a12345678

[server]
# 多进程处理数量
multiprocess_count = 4
# 日志路径（强制）
log_path = /sdata/var/log/CollectionLog/server.log
# 是否开启报警（0：不开启）
alarms_status = 1
# 钉钉报警地址（钉钉机器人的报警地址）
alarms_jmc_url = https://??????
# 报警内部前缀，前缀自带换行（比如：日志同步工具【server】）（也适合用钉钉报警需要的关键字）
alarms_jmc_prefix = 日志同步工具【server端】
```



### client

``` ini
[redis]
host = "127.0.0.1:6379"
# 没密码为空
pass = a12345678

[log]
# name：项目名称
# source_path：源日志目录，自动获取最新有写入的日志文件
# target_path：目标存储目录，存储方式：/tmp/log/20220307/msg/.......
# 规则：先用target_path作为日志存储主目录，用name来区分业务项目，加上当天时间，会对source_path进行分割，获取到的source_path之后的路径，进行拼接
# 比如获取到文件为/tmp/da/log/20220318.log，那么解析写到日志服务器的路径为：/tmp/log/20220318/msg/log/20220318.log（/tmp/log/当天时间/项目名称/log/20220318.log）
conf = [{"name": "msg", "source_path":"/tmp/da", "target_path": "/tmp/log"}]

[file]
file_record_time = 28800 # 解析文件，文件指针记录保留时间（秒）
file_time = 1 #轮查时间（秒）
file_path = ./seek.ini #存储实时获取的文件指针


[server]
# 日志路径（强制）
log_path = /sdata/var/log/CollectionLog/client.log
# 是否开启报警（0：不开启）
alarms_status = 1
# 钉钉报警地址
alarms_jmc_url = https://??????
# 报警内部前缀，前缀自带换行（比如：日志同步工具【client端-web机器】）（也适合用钉钉报警需要的关键字）
alarms_jmc_prefix = 日志同步工具【client端-数据处理机】
```





# 版本变化

1. v1.1：基本的日志实时同步、监控报警、日志记录、日志文件被重写时重新获取(针对一些架构会对日志文件达到一定的大小后，将其切割)



# PS

目前该项目已在本司使用，日志量不大，每日日志量在20G左右



