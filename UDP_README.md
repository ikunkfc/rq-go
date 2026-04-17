# UDP File Transfer with RaptorQ

这个目录包含基于 RaptorQ 编码的 UDP 文件传输系统，包括发送端和接收端服务。

[English](#english) | [中文](#chinese)

---

<a name="chinese"></a>
## 中文文档

### 概述

这个系统使用 RaptorQ 前向纠错编码通过 UDP 协议可靠地传输文件。即使在丢包的情况下，接收端也能恢复原始文件。

### 组件

- **udp-sender**: 发送端服务，将文件编码为 RaptorQ 符号并通过 UDP 发送
- **udp-receiver**: 接收端服务，接收 UDP 数据包并解码还原为原始文件

### 构建

```bash
# 构建发送端
cd udp-sender
go build -o udp-sender

# 构建接收端
cd udp-receiver
go build -o udp-receiver
```

### 使用方法

#### 1. 启动接收端

首先在接收端机器上启动接收服务：

```bash
./udp-receiver -port 9000 -output ./received -verbose
```

**接收端参数：**

- `-port`: UDP 监听端口（默认：9000）
- `-output`: 接收文件的输出目录（默认：./received）
- `-buffer`: UDP 接收缓冲区大小（字节）（默认：65536）
- `-timeout`: 接收数据超时时间（秒）（默认：300）
- `-memory`: 最大内存使用量（MB）（默认：512）
- `-verbose`: 启用详细日志输出

**示例输出：**
```
[INIT] UDP Receiver starting...
[CONFIG] Port: 9000
[CONFIG] Output directory: ./received
[CONFIG] Buffer size: 65536 bytes
[CONFIG] Timeout: 300 seconds
[CONFIG] Verbose logging: true
[SUCCESS] Output directory created/verified: ./received
[SUCCESS] UDP receiver listening on :9000
[WAITING] Waiting for incoming data...
```

#### 2. 发送文件

在发送端机器上启动发送服务：

```bash
./udp-sender -file myfile.dat -host 192.168.1.100 -port 9000 -verbose
```

**发送端参数：**

- `-file`: 要发送的输入文件（必需）
- `-host`: 目标主机地址（默认：localhost）
- `-port`: 目标 UDP 端口（默认：9000）
- `-blocksize`: 块大小（MB）（默认：0，自动）
- `-buffer`: UDP 发送缓冲区大小（字节）（默认：65536）
- `-rate`: 速率限制（Mbps）（默认：0，不限速）
- `-delay`: 数据包之间的延迟（微秒）（默认：0）
- `-memory`: 最大内存使用量（MB）（默认：512）
- `-verbose`: 启用详细日志输出

**示例输出：**
```
[INIT] UDP Sender starting...
[CONFIG] Input file: myfile.dat
[CONFIG] Target: 192.168.1.100:9000
[INFO] Input file size: 1048576 bytes (1.00 MB)
[ENCODE] Starting file encoding...
[SUCCESS] RaptorQ processor created
[ENCODE] Encoding file to RaptorQ symbols...
[SUCCESS] Encoding completed in 234ms
[INFO] Total symbols generated: 42
[INFO] Total repair symbols: 10
[SEND] Starting data transmission...
[SUCCESS] Connected to 192.168.1.100:9000
[SEND] Sending metadata packet...
[SUCCESS] Metadata packet sent
[SEND] Sending 42 symbol packets...
[PROGRESS] 100.0% (42/42 symbols) - 85.32 Mbps
[SUCCESS] All symbols sent
[SEND] Sending end of transmission packet...
[SUCCESS] End of transmission packet sent
[STATS] Total transmission time: 1.234s
[STATS] Total bytes sent: 2785642 (2.66 MB)
[STATS] Average transmission rate: 85.32 Mbps
[SUCCESS] File sent successfully!
```

### 工作原理

1. **编码阶段**（发送端）：
   - 使用 RaptorQ 将输入文件编码为多个符号
   - 生成额外的修复符号以实现容错
   - 创建元数据文件以记录编码参数

2. **传输阶段**（发送端）：
   - 首先发送元数据包
   - 逐个发送所有符号数据包
   - 最后发送传输结束标记

3. **接收阶段**（接收端）：
   - 监听 UDP 端口接收数据包
   - 保存元数据和符号文件
   - 收到结束标记或超时后开始解码

4. **解码阶段**（接收端）：
   - 使用 RaptorQ 将接收到的符号解码为原始文件
   - 即使丢失部分数据包也能恢复文件（取决于冗余度）

### 数据包格式

每个 UDP 数据包包含以下头部（13 字节）：

| 字段 | 大小 | 描述 |
|------|------|------|
| PacketType | 1 字节 | 0=元数据, 1=符号数据, 2=传输结束 |
| SequenceNum | 4 字节 | 数据包序列号 |
| TotalPackets | 4 字节 | 预期的总数据包数 |
| DataLen | 4 字节 | 此数据包中的数据长度 |
| Payload | 可变 | 实际数据 |

### 高级用法

#### 限制发送速率

如果网络带宽有限或想要避免拥塞：

```bash
# 限制为 10 Mbps
./udp-sender -file largefile.bin -host 192.168.1.100 -rate 10

# 或使用数据包延迟（1000 微秒 = 1 毫秒）
./udp-sender -file largefile.bin -host 192.168.1.100 -delay 1000
```

#### 自定义块大小

对于大文件，可以指定块大小以优化内存使用：

```bash
# 使用 100 MB 块大小
./udp-sender -file hugefile.bin -host 192.168.1.100 -blocksize 100
```

#### 调整缓冲区大小

对于高速网络，可以增加缓冲区大小：

```bash
# 发送端
./udp-sender -file myfile.dat -host 192.168.1.100 -buffer 131072

# 接收端
./udp-receiver -port 9000 -buffer 131072
```

#### 内存使用控制

**重要**：对于内存受限的服务器（如 1GB RAM），务必设置适当的内存限制：

```bash
# 低内存服务器（1GB RAM）- 使用 256MB 内存限制
./udp-sender -file largefile.bin -host 192.168.1.100 -memory 256

# 接收端也要设置相同的内存限制
./udp-receiver -port 9000 -memory 256
```

**内存使用说明：**

- 默认内存限制为 512MB，适合大多数服务器
- 对于 1GB RAM 的服务器，建议设置 `-memory 256` 或更低
- 对于 512MB RAM 的服务器，建议设置 `-memory 128`
- 系统会根据内存限制自动计算合适的块大小
- 较小的内存限制会导致文件被分成更多块处理，编码时间会增加但内存占用降低

**大文件传输优化：**

```bash
# 传输大文件（如 1GB+），在低内存服务器上
./udp-sender -file large.iso -host 192.168.1.100 -memory 256 -blocksize 50

# blocksize 参数可以手动控制每个块的大小（MB）
# 较小的块大小 = 更低的内存占用 + 更多的编码时间
```

---

<a name="english"></a>
## English Documentation

### Overview

This system provides reliable file transfer over UDP using RaptorQ Forward Error Correction (FEC) encoding. Files can be recovered at the receiver even with packet loss.

### Components

- **udp-sender**: Sender service that encodes files into RaptorQ symbols and transmits via UDP
- **udp-receiver**: Receiver service that receives UDP packets and decodes them back to the original file

### Building

```bash
# Build sender
cd udp-sender
go build -o udp-sender

# Build receiver
cd udp-receiver
go build -o udp-receiver
```

### Usage

#### 1. Start the Receiver

First, start the receiver service on the receiving machine:

```bash
./udp-receiver -port 9000 -output ./received -verbose
```

**Receiver Parameters:**

- `-port`: UDP port to listen on (default: 9000)
- `-output`: Output directory for received files (default: ./received)
- `-buffer`: UDP receive buffer size in bytes (default: 65536)
- `-timeout`: Timeout in seconds for receiving data (default: 300)
- `-memory`: Max memory usage in MB (default: 512)
- `-verbose`: Enable verbose logging

**Sample Output:**
```
[INIT] UDP Receiver starting...
[CONFIG] Port: 9000
[CONFIG] Output directory: ./received
[CONFIG] Buffer size: 65536 bytes
[CONFIG] Timeout: 300 seconds
[CONFIG] Verbose logging: true
[SUCCESS] Output directory created/verified: ./received
[SUCCESS] UDP receiver listening on :9000
[WAITING] Waiting for incoming data...
```

#### 2. Send a File

On the sender machine, start the sender service:

```bash
./udp-sender -file myfile.dat -host 192.168.1.100 -port 9000 -verbose
```

**Sender Parameters:**

- `-file`: Input file to send (required)
- `-host`: Target host address (default: localhost)
- `-port`: Target UDP port (default: 9000)
- `-blocksize`: Block size in MB (default: 0 for auto)
- `-buffer`: UDP send buffer size in bytes (default: 65536)
- `-rate`: Rate limit in Mbps (default: 0 for unlimited)
- `-delay`: Delay between packets in microseconds (default: 0)
- `-memory`: Max memory usage in MB (default: 512)
- `-verbose`: Enable verbose logging

**Sample Output:**
```
[INIT] UDP Sender starting...
[CONFIG] Input file: myfile.dat
[CONFIG] Target: 192.168.1.100:9000
[INFO] Input file size: 1048576 bytes (1.00 MB)
[ENCODE] Starting file encoding...
[SUCCESS] RaptorQ processor created
[ENCODE] Encoding file to RaptorQ symbols...
[SUCCESS] Encoding completed in 234ms
[INFO] Total symbols generated: 42
[INFO] Total repair symbols: 10
[SEND] Starting data transmission...
[SUCCESS] Connected to 192.168.1.100:9000
[SEND] Sending metadata packet...
[SUCCESS] Metadata packet sent
[SEND] Sending 42 symbol packets...
[PROGRESS] 100.0% (42/42 symbols) - 85.32 Mbps
[SUCCESS] All symbols sent
[SEND] Sending end of transmission packet...
[SUCCESS] End of transmission packet sent
[STATS] Total transmission time: 1.234s
[STATS] Total bytes sent: 2785642 (2.66 MB)
[STATS] Average transmission rate: 85.32 Mbps
[SUCCESS] File sent successfully!
```

### How It Works

1. **Encoding Phase** (Sender):
   - Encodes the input file into RaptorQ symbols
   - Generates additional repair symbols for fault tolerance
   - Creates metadata file with encoding parameters

2. **Transmission Phase** (Sender):
   - Sends metadata packet first
   - Sends all symbol data packets sequentially
   - Sends end-of-transmission marker

3. **Reception Phase** (Receiver):
   - Listens on UDP port for incoming packets
   - Saves metadata and symbol files
   - Starts decoding after receiving EOT or timeout

4. **Decoding Phase** (Receiver):
   - Uses RaptorQ to decode received symbols back to original file
   - Can recover file even with some packet loss (depending on redundancy)

### Packet Format

Each UDP packet contains a header (13 bytes):

| Field | Size | Description |
|-------|------|-------------|
| PacketType | 1 byte | 0=metadata, 1=symbol data, 2=end of transmission |
| SequenceNum | 4 bytes | Packet sequence number |
| TotalPackets | 4 bytes | Total expected packets |
| DataLen | 4 bytes | Length of data in this packet |
| Payload | Variable | Actual data |

### Advanced Usage

#### Rate Limiting

To limit transmission rate for bandwidth-constrained networks:

```bash
# Limit to 10 Mbps
./udp-sender -file largefile.bin -host 192.168.1.100 -rate 10

# Or use packet delay (1000 microseconds = 1 millisecond)
./udp-sender -file largefile.bin -host 192.168.1.100 -delay 1000
```

#### Custom Block Size

For large files, specify block size to optimize memory usage:

```bash
# Use 100 MB block size
./udp-sender -file hugefile.bin -host 192.168.1.100 -blocksize 100
```

#### Adjust Buffer Size

For high-speed networks, increase buffer size:

```bash
# Sender
./udp-sender -file myfile.dat -host 192.168.1.100 -buffer 131072

# Receiver
./udp-receiver -port 9000 -buffer 131072
```

#### Memory Usage Control

**IMPORTANT**: For memory-constrained servers (e.g., 1GB RAM), set appropriate memory limits:

```bash
# Low-memory server (1GB RAM) - use 256MB memory limit
./udp-sender -file largefile.bin -host 192.168.1.100 -memory 256

# Receiver should also use the same memory limit
./udp-receiver -port 9000 -memory 256
```

**Memory Usage Guidelines:**

- Default memory limit is 512MB, suitable for most servers
- For 1GB RAM servers, recommend `-memory 256` or lower
- For 512MB RAM servers, recommend `-memory 128`
- The system automatically calculates appropriate block size based on memory limit
- Lower memory limits result in more blocks and longer encoding time, but lower memory footprint

**Large File Transfer Optimization:**

```bash
# Transfer large files (e.g., 1GB+) on low-memory servers
./udp-sender -file large.iso -host 192.168.1.100 -memory 256 -blocksize 50

# blocksize parameter manually controls block size (MB)
# Smaller block size = lower memory usage + longer encoding time
```

### Troubleshooting

**Problem: Receiver timeout**
- Solution: Increase timeout value with `-timeout` flag
- Check network connectivity between sender and receiver
- Verify firewall rules allow UDP traffic on the specified port

**Problem: Decoding fails**
- Solution: Too many packets were lost
- Increase redundancy factor by modifying the RaptorQ processor settings
- Reduce sending rate to avoid network congestion

**Problem: High packet loss**
- Solution: Use rate limiting (`-rate` or `-delay` flags)
- Increase UDP buffer sizes
- Check network quality and capacity

**Problem: High memory usage or out of memory errors**
- Solution: Reduce memory limit with `-memory` flag (e.g., `-memory 256` for 1GB RAM servers)
- Manually set smaller block size with `-blocksize` flag
- For very large files on low-memory systems, use smaller blocks (e.g., `-blocksize 20`)
- Monitor system memory usage during transfer

### Performance Tips

1. **For local network (LAN)**: Use default settings or increase buffer sizes
2. **For WAN/Internet**: Use rate limiting and increase redundancy
3. **For large files**: Specify appropriate block size
4. **For debugging**: Enable verbose logging with `-verbose`

### Examples

#### Local Network Transfer
```bash
# Terminal 1 (Receiver)
./udp-receiver -port 9000 -output ./received

# Terminal 2 (Sender)
./udp-sender -file movie.mp4 -host localhost -port 9000
```

#### Remote Transfer with Rate Limiting
```bash
# On remote server (Receiver)
./udp-receiver -port 9000 -output /data/received -timeout 600

# On local machine (Sender)
./udp-sender -file backup.tar.gz -host server.example.com -port 9000 -rate 50 -verbose
```

#### Large File Transfer
```bash
# Receiver with extended timeout
./udp-receiver -port 9000 -output ./received -timeout 1800

# Sender with custom block size
./udp-sender -file large_dataset.bin -host 192.168.1.100 -blocksize 200
```
