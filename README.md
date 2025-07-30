# MCP_Ollama_Scanner_server 节点扫描工具使用说明

![Ollama Scanner](images/README/1739551751297.png)

[English](README_en.md) · [简体中文](README.md)

## 工具概述

- 一个用于局域网扫描 Ollama Scanner 节点的工具，具备自动执行性能测试并将结果导出到 CSV 文件的功能。它借助 zmap 工具来扫描 IP 地址，同时对每个 IP 地址上的 Ollama 服务进行检查，获取模型信息并开展性能测试。
- 也可以用于嗅探公网IP中是否具有Ollama服务
- 分支MCP_Ollama_Scanner,重构业务，提供MCP基础服务，为模型提供对接业务。通用性，可和标准MCP服务进行对接

## 使用方法

### 如何获取网关mac地址

- 如何获取网关mac地址:

```bash
ip addr show | grep link/ether
```

- 或者输入ip addr show,查看你的访问外网的那个网卡的mac地址

### 支持WebUI访问模式

- 将扫描结果导出到数据库liteSQL
- 支持以WebUI的形式查询扫描结果

### Windows下使用方案

由于win系统下是无法安装zmap的，所以win下有下面几种使用方式：

- 使用masscan版本的嗅探器(通用性更佳-推荐)
- 使用wsl模式运行linux版本嗅探器
- 使用docker容器

  ```docker
  # 下载镜像
  docker pull docker.io/aspnmy/ollama_scanner:v2.2-zmap
  # 运行嗅探器
  docker exec -it [dockerid]   /usr/local/bin/ollama_scanner  [参数]
  ```

### 基本用法

- 在命令行中运行该工具时，可使用如下基本命令格式：

```bash
./ollama_scanner [参数]
```

### 参数说明


| 参数         | 描述                                             | 默认值                         |
| ------------ | ------------------------------------------------ | ------------------------------ |
| -gateway-mac | 指定网关 MAC 地址，格式为 aa:bb:cc:dd:ee:ff      | 无（必须指定）                 |
| -input       | 输入文件路径，文件内容为 CIDR 格式的 IP 地址列表 | ip.txt                         |
| -output      | CSV 输出文件路径                                 | results.csv                    |
| -no-bench    | 禁用性能基准测试                                 | false                          |
| -prompt      | 性能测试提示词                                   | 为什么太阳会发光？用一句话回答 |
| -T           | zmap 线程数                                      | 10                             |

### 使用示例

- 指定Ip地址，禁用性能测试，并指定输出文件，并指定 zmap 线程数：

```bash
./ollama_scanner -input ip.txt  -no-bench -output custom.csv -T 20
```

- 指定 IP 地址列表进行扫描：

```bash
./ollama_scanner -input ip.txt
```

- 指定网关 MAC 地址进行扫描：

```bash
./ollama_scanner -gateway-mac aa:bb:cc:dd:ee:ff
```

- 指定网关 MAC 地址，禁用性能测试，并指定输出文件：

```bash
./ollama_scanner -gateway-mac aa:bb:cc:dd:ee:ff -no-bench -output custom.csv
```

- 指定网关 MAC 地址和 zmap 线程数：

```bash
./ollama_scanner -gateway-mac aa:bb:cc:dd:ee:ff -T 20
```

### 工具执行流程

#### 初始化：解析命令行参数，创建可取消的上下文，检查并安装 zmap（若未安装），初始化 CSV 写入器，设置信号处理。

#### 扫描过程：

- 验证输入参数的有效性。
- 执行 zmap 扫描，获取存活的 IP 地址。
- 对每个存活的 IP 地址进行端口检查和 Ollama 服务检查。
- 获取每个 IP 地址上的模型信息。
- 若未禁用性能测试，则对每个模型进行性能测试。

#### 结果处理：

- 将扫描结果打印到控制台。
- 将扫描结果写入 CSV 文件。

### 注意事项

- zmap 或 masscan安装：工具会尝试自动安装 zmap或masscan，不过在某些操作系统上可能需要手动安装。若自动安装失败，工具会提示你手动安装并提供安装链接。
- 输入文件：输入文件需包含 CIDR 格式的 IP 地址列表，若文件不存在，工具会报错。
- 性能测试：性能测试可能会消耗较多时间和资源，你可以使用 -no-bench 参数禁用该功能。

## 如何编译程序本体

- 增加编译 arm64平台嗅探器，arm64架构的程序本体可以直接运行或者直接拉取docker镜像
- 编译所有平台的程序：在终端中运行 make 或 make all 命令，将分别为 macOS、Linux 和 Windows 平台生成对应的可执行文件。
- 单独编译某个平台的程序：

  - 编译 macOS 平台：make build-macos
  - 编译 Linux 平台：make build-linux
  - 编译 Windows 平台：make build-windows
- 清理生成的文件：运行 make clean 命令，将删除所有生成的可执行文件。
- 请确保你的系统已经安装了 Go 环境，并且 go 命令可以在终端中正常使用。

## 如何编译程序docker镜像

- git本项目，将env.json.def文件修改成env.json,修改里面的参数

  ```bash
  mv env.json.def env.json
  ```
- 运行bash build.sh

  ```bash
  bash build.sh
  ```

## 沟通与交流

- https://t.me/+YfCVhGWyKxoyMDhl
