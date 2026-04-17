# 低内存服务器使用指南 / Low Memory Server Guide

## 问题 / Problem

在内存有限的服务器（如 1GB RAM）上发送大文件时，UDP sender 会占用大量内存并生成大量小文件。

When sending large files on memory-constrained servers (e.g., 1GB RAM), UDP sender consumes excessive memory and generates many small files.

## 解决方案 / Solution

### 1. 设置内存限制 / Set Memory Limit

对于 1GB RAM 的服务器，使用 256MB 或更低的内存限制：

For 1GB RAM servers, use 256MB or lower memory limit:

```bash
# 发送端 / Sender
./udp-sender -file yourfile.bin -host TARGET_IP -memory 256

# 接收端 / Receiver
./udp-receiver -port 9000 -memory 256
```

对于 512MB RAM 的服务器：

For 512MB RAM servers:

```bash
./udp-sender -file yourfile.bin -host TARGET_IP -memory 128
./udp-receiver -port 9000 -memory 128
```

### 2. 手动设置块大小 / Manually Set Block Size

对于非常大的文件，可以手动指定较小的块大小：

For very large files, manually specify smaller block size:

```bash
# 传输 1GB+ 文件，使用 20-50MB 块大小
# Transfer 1GB+ files with 20-50MB block size
./udp-sender -file large.iso -host TARGET_IP -memory 256 -blocksize 30
```

### 3. 内存限制与块大小对照表 / Memory Limit vs Block Size Reference

| 服务器内存<br/>Server RAM | 推荐 -memory<br/>Recommended | 推荐 -blocksize<br/>Recommended | 说明<br/>Notes |
|------------------------|---------------------------|-------------------------------|-------------|
| 512MB                  | 128                       | 10-20                         | 最小配置 / Minimal |
| 1GB                    | 256                       | 20-50                         | 低内存 / Low memory |
| 2GB                    | 512 (默认)                 | 50-100                        | 标准配置 / Standard |
| 4GB+                   | 1024-2048                 | 100+ (自动)                    | 推荐配置 / Recommended |

### 4. 工作原理 / How It Works

**内存限制的作用：**

1. 限制 RaptorQ 编码器的最大内存使用
2. 系统自动计算合适的块大小（块大小 ≈ 内存限制 / 4）
3. 文件被分成多个块分别处理，每个块独立编码

**Memory limit effects:**

1. Limits max memory usage of RaptorQ encoder
2. System automatically calculates appropriate block size (block size ≈ memory limit / 4)
3. File is split into blocks, each encoded independently

**为什么会生成大量文件？**

使用 1400 字节的小符号大小时：
- 1MB 数据 → 约 750 个符号文件
- 100MB 数据 → 约 75,000 个符号文件
- 1GB 数据 → 约 750,000 个符号文件

通过设置块大小，文件被分成多个块，每个块处理完后释放内存，避免一次性生成过多文件。

**Why so many files?**

With 1400-byte symbol size:
- 1MB data → ~750 symbol files
- 100MB data → ~75,000 symbol files
- 1GB data → ~750,000 symbol files

By setting block size, file is split into blocks, each processed and freed, avoiding generating too many files at once.

### 5. 实际示例 / Practical Examples

#### 示例 1: 传输 500MB 文件，1GB RAM 服务器

```bash
# 发送端 / Sender
./udp-sender -file 500mb.bin -host 192.168.1.100 -memory 256 -blocksize 50 -verbose

# 接收端 / Receiver
./udp-receiver -port 9000 -memory 256 -output ./received -verbose
```

**预期结果 / Expected:**
- 文件分成约 10 个块（500MB / 50MB）
- 每个块约 37,500 个符号文件（50MB / 1400 bytes）
- 峰值内存使用 < 256MB
- 每个块处理完会清理临时文件

#### 示例 2: 传输 2GB 文件，1GB RAM 服务器

```bash
# 发送端 / Sender
./udp-sender -file 2gb.iso -host 192.168.1.100 -memory 256 -blocksize 30 -verbose

# 接收端 / Receiver
./udp-receiver -port 9000 -memory 256 -output ./received -verbose
```

**预期结果 / Expected:**
- 文件分成约 68 个块（2GB / 30MB）
- 每个块约 22,500 个符号文件（30MB / 1400 bytes）
- 峰值内存使用 < 256MB
- 编码时间会增加，但内存安全

### 6. 监控和优化 / Monitoring and Optimization

**监控内存使用 / Monitor memory usage:**

```bash
# Linux
watch -n 1 free -h
top -p $(pgrep udp-sender)

# 或使用 htop
htop
```

**如果仍然内存不足 / If still out of memory:**

1. 进一步减小内存限制：`-memory 128` 或 `-memory 64`
2. 进一步减小块大小：`-blocksize 10` 或 `-blocksize 5`
3. 关闭其他占用内存的程序
4. 考虑升级服务器内存

**优化传输速度 / Optimize transfer speed:**

```bash
# 如果网络良好，可以增加发送速率
./udp-sender ... -delay 100  # 减小延迟

# 如果网络不稳定，增加延迟
./udp-sender ... -delay 5000  # 增加延迟到 5ms
```

### 7. 故障排查 / Troubleshooting

**问题：进程被 killed (OOM killer)**
```
Killed
```

**解决方案：**
- 减小 `-memory` 值到 128 或更低
- 减小 `-blocksize` 值到 10-20
- 检查系统其他进程的内存占用

---

**问题：编码速度很慢**

**解决方案：**
- 这是正常的，较小的块大小需要更多时间
- 可以适当增加块大小（在内存允许范围内）
- 或者使用更强大的服务器

---

**问题：大量 "symbol_xxxxx" 文件**

**解决方案：**
- 这是正常的，每个块会生成符号文件
- 文件在发送完成后会自动清理
- 临时文件位于 `/tmp/udp-sender-*` 目录

## 总结 / Summary

对于 1GB RAM 的服务器传输大文件：

For 1GB RAM servers transferring large files:

```bash
# 推荐配置 / Recommended configuration
./udp-sender -file yourfile -host IP -memory 256 -blocksize 30-50
./udp-receiver -memory 256
```

这样可以：
- ✅ 控制内存使用在安全范围内
- ✅ 避免 OOM killer 终止进程
- ✅ 成功传输大文件
- ⚠️ 编码时间会增加（可接受的代价）

This ensures:
- ✅ Memory usage within safe limits
- ✅ Avoid OOM killer terminating process
- ✅ Successfully transfer large files
- ⚠️ Encoding time increases (acceptable trade-off)
