package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	raptorq "github.com/LumeraProtocol/rq-go"
)

// PacketHeader contains metadata about the packet
type PacketHeader struct {
	PacketType   uint8  // 0: metadata, 1: symbol data, 2: end of transmission
	SequenceNum  uint32 // Packet sequence number
	TotalPackets uint32 // Total number of packets to expect
	DataLen      uint32 // Length of data in this packet
}

// Config holds the sender configuration
type Config struct {
	InputFile     string
	Host          string
	Port          int
	BlockSize     int
	BufferSize    int
	RateLimit     int
	PacketDelay   int
	MaxMemoryMB   int
	VerboseLog    bool
}

func main() {
	// Parse command line arguments
	config := Config{}
	flag.StringVar(&config.InputFile, "file", "", "Input file to send (required)")
	flag.StringVar(&config.Host, "host", "localhost", "Target host address")
	flag.IntVar(&config.Port, "port", 9000, "Target UDP port")
	flag.IntVar(&config.BlockSize, "blocksize", 0, "Block size in MB (0 for auto)")
	flag.IntVar(&config.BufferSize, "buffer", 65536, "UDP send buffer size in bytes")
	flag.IntVar(&config.RateLimit, "rate", 0, "Rate limit in Mbps (0 for unlimited)")
	flag.IntVar(&config.PacketDelay, "delay", 0, "Delay between packets in microseconds")
	flag.IntVar(&config.MaxMemoryMB, "memory", 512, "Max memory usage in MB")
	flag.BoolVar(&config.VerboseLog, "verbose", false, "Enable verbose logging")
	flag.Parse()

	// Validate required arguments
	if config.InputFile == "" {
		log.Fatal("[ERROR] Input file is required. Use -file flag to specify.")
	}

	log.Printf("[INIT] UDP Sender starting...")
	log.Printf("[CONFIG] Input file: %s", config.InputFile)
	log.Printf("[CONFIG] Target: %s:%d", config.Host, config.Port)
	log.Printf("[CONFIG] Block size: %d MB (0=auto)", config.BlockSize)
	log.Printf("[CONFIG] Buffer size: %d bytes", config.BufferSize)
	log.Printf("[CONFIG] Rate limit: %d Mbps (0=unlimited)", config.RateLimit)
	log.Printf("[CONFIG] Packet delay: %d microseconds", config.PacketDelay)
	log.Printf("[CONFIG] Max memory: %d MB", config.MaxMemoryMB)
	log.Printf("[CONFIG] Verbose logging: %v", config.VerboseLog)

	// Check if input file exists
	fileInfo, err := os.Stat(config.InputFile)
	if err != nil {
		log.Fatalf("[ERROR] Failed to access input file: %v", err)
	}
	log.Printf("[INFO] Input file size: %d bytes (%.2f MB)", fileInfo.Size(), float64(fileInfo.Size())/(1024*1024))

	// Encode the file
	symbolsDir, layoutPath, err := encodeFile(&config)
	if err != nil {
		log.Fatalf("[ERROR] Encoding failed: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(symbolsDir)) // Clean up temporary directory

	// Send the encoded data
	if err := sendData(&config, symbolsDir, layoutPath); err != nil {
		log.Fatalf("[ERROR] Sending failed: %v", err)
	}

	log.Printf("[SUCCESS] File sent successfully!")
}

func encodeFile(config *Config) (string, string, error) {
	log.Printf("[ENCODE] Starting file encoding...")

	// Create RaptorQ processor with smaller symbol size for UDP transmission
	// Use 1400 bytes to stay well under UDP MTU (typically 1500 bytes, minus headers)
	// Use configurable memory limit to support low-memory servers
	processor, err := raptorq.NewRaptorQProcessor(
		1400,                        // Symbol size: 1400 bytes (safe for UDP)
		4,                           // Redundancy factor
		uint64(config.MaxMemoryMB), // Max memory from config
		4,                           // Concurrency limit
	)
	if err != nil {
		return "", "", fmt.Errorf("failed to create RaptorQ processor: %v", err)
	}
	defer processor.Free()
	log.Printf("[SUCCESS] RaptorQ processor created with 1400-byte symbols, %dMB memory limit", config.MaxMemoryMB)

	// Create temporary directory for symbols
	tmpDir, err := os.MkdirTemp("", "udp-sender-*")
	if err != nil {
		return "", "", fmt.Errorf("failed to create temp directory: %v", err)
	}
	symbolsDir := filepath.Join(tmpDir, "symbols")
	log.Printf("[INFO] Temporary symbols directory: %s", symbolsDir)

	if err := os.MkdirAll(symbolsDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create symbols directory: %v", err)
	}

	// Calculate block size
	blockSize := config.BlockSize * 1024 * 1024 // Convert MB to bytes
	if blockSize == 0 {
		fileInfo, _ := os.Stat(config.InputFile)
		fileSize := uint64(fileInfo.Size())

		// Calculate appropriate block size based on memory constraints
		// For UDP with 1400-byte symbols, we want to limit the number of symbols per block
		// Aim for blocks that create ~10000-50000 symbols each to balance memory and efficiency
		recommendedBlockSize := processor.GetRecommendedBlockSize(fileSize)

		// If file is large and recommended size is 0 or too small, calculate based on memory
		if recommendedBlockSize == 0 || (fileSize > 100*1024*1024 && recommendedBlockSize < 10*1024*1024) {
			// Use a fraction of available memory for each block
			// Reserve memory for: source data + encoded symbols + overhead
			// Each 1MB of data creates ~750 symbols (1MB / 1400 bytes), each symbol ~1400 bytes
			// Total memory per MB ≈ 1MB (source) + 750*1400 (symbols) ≈ 2MB
			maxBlockSizeFromMemory := uint64(config.MaxMemoryMB) * 1024 * 1024 / 4 // Use 1/4 of available memory

			// Choose a reasonable block size (10-100 MB range)
			blockSize = int(maxBlockSizeFromMemory)
			if blockSize < 10*1024*1024 {
				blockSize = 10 * 1024 * 1024 // Minimum 10MB
			}
			if blockSize > 100*1024*1024 {
				blockSize = 100 * 1024 * 1024 // Maximum 100MB
			}
			log.Printf("[INFO] Calculated block size: %d bytes (%.2f MB) based on %dMB memory limit",
				blockSize, float64(blockSize)/(1024*1024), config.MaxMemoryMB)
		} else {
			blockSize = int(recommendedBlockSize)
			log.Printf("[INFO] Using recommended block size: %d bytes (%.2f MB)", blockSize, float64(blockSize)/(1024*1024))
		}
	} else {
		log.Printf("[INFO] Using specified block size: %d bytes (%.2f MB)", blockSize, float64(blockSize)/(1024*1024))
	}

	// Encode the file
	log.Printf("[ENCODE] Encoding file to RaptorQ symbols...")
	startTime := time.Now()
	result, err := processor.EncodeFile(config.InputFile, symbolsDir, blockSize)
	if err != nil {
		return "", "", fmt.Errorf("encoding failed: %v", err)
	}
	elapsed := time.Since(startTime)

	log.Printf("[SUCCESS] Encoding completed in %v", elapsed)
	log.Printf("[INFO] Total symbols generated: %d", result.TotalSymbolsCount)
	log.Printf("[INFO] Total repair symbols: %d", result.TotalRepairSymbols)
	log.Printf("[INFO] Layout file: %s", result.LayoutFilePath)

	return symbolsDir, result.LayoutFilePath, nil
}

func sendData(config *Config, symbolsDir, layoutPath string) error {
	log.Printf("[SEND] Starting data transmission...")

	// Connect to target
	addr := fmt.Sprintf("%s:%d", config.Host, config.Port)
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %v", addr, err)
	}
	defer conn.Close()
	log.Printf("[SUCCESS] Connected to %s", addr)

	// Read layout file (metadata)
	metadata, err := os.ReadFile(layoutPath)
	if err != nil {
		return fmt.Errorf("failed to read layout file: %v", err)
	}
	log.Printf("[INFO] Metadata size: %d bytes", len(metadata))

	// Get list of symbol files (including in subdirectories)
	var symbols []string
	err = filepath.Walk(symbolsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip directories and the layout file
		if !info.IsDir() && filepath.Base(path) != "_raptorq_layout.json" {
			symbols = append(symbols, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to list symbol files: %v", err)
	}

	totalPackets := uint32(1 + len(symbols) + 1) // metadata + symbols + EOT
	log.Printf("[INFO] Total packets to send: %d (1 metadata + %d symbols + 1 EOT)", totalPackets, len(symbols))

	startTime := time.Now()
	var totalBytesSent uint64

	// Send metadata packet
	log.Printf("[SEND] Sending metadata packet...")
	if err := sendPacket(conn, 0, 0, totalPackets, metadata); err != nil {
		return fmt.Errorf("failed to send metadata: %v", err)
	}
	totalBytesSent += uint64(len(metadata) + 13) // 13 bytes header
	log.Printf("[SUCCESS] Metadata packet sent")

	if config.PacketDelay > 0 {
		time.Sleep(time.Duration(config.PacketDelay) * time.Microsecond)
	}

	// Send symbol packets
	log.Printf("[SEND] Sending %d symbol packets...", len(symbols))
	for i, symbolFile := range symbols {
		data, err := os.ReadFile(symbolFile)
		if err != nil {
			log.Printf("[WARNING] Failed to read symbol file %s: %v", symbolFile, err)
			continue
		}

		// Get relative path from symbolsDir
		relPath, err := filepath.Rel(symbolsDir, symbolFile)
		if err != nil {
			log.Printf("[WARNING] Failed to get relative path for %s: %v", symbolFile, err)
			continue
		}

		// Prepend filename length and filename to data
		filenameBytes := []byte(relPath)
		filenameLen := uint16(len(filenameBytes))
		packetData := make([]byte, 2+len(filenameBytes)+len(data))
		packetData[0] = byte(filenameLen >> 8)
		packetData[1] = byte(filenameLen)
		copy(packetData[2:], filenameBytes)
		copy(packetData[2+len(filenameBytes):], data)

		seqNum := uint32(i + 1)
		if err := sendPacket(conn, 1, seqNum, totalPackets, packetData); err != nil {
			log.Printf("[WARNING] Failed to send symbol %d: %v", seqNum, err)
			continue
		}

		totalBytesSent += uint64(len(packetData) + 13)

		if config.VerboseLog {
			log.Printf("[PROGRESS] Sent symbol %d/%d (%s)", seqNum, len(symbols), relPath)
		} else if (i+1)%100 == 0 || i == len(symbols)-1 {
			elapsed := time.Since(startTime)
			rate := float64(totalBytesSent*8) / elapsed.Seconds() / 1000000 // Mbps
			progress := float64(i+1) / float64(len(symbols)) * 100
			log.Printf("[PROGRESS] %.1f%% (%d/%d symbols) - %.2f Mbps", progress, i+1, len(symbols), rate)
		}

		// Apply rate limiting
		if config.PacketDelay > 0 {
			time.Sleep(time.Duration(config.PacketDelay) * time.Microsecond)
		}
	}

	log.Printf("[SUCCESS] All symbols sent")

	// Send end of transmission packet
	log.Printf("[SEND] Sending end of transmission packet...")
	if err := sendPacket(conn, 2, totalPackets-1, totalPackets, []byte{}); err != nil {
		return fmt.Errorf("failed to send EOT: %v", err)
	}
	log.Printf("[SUCCESS] End of transmission packet sent")

	elapsed := time.Since(startTime)
	avgRate := float64(totalBytesSent*8) / elapsed.Seconds() / 1000000 // Mbps

	log.Printf("[STATS] Total transmission time: %v", elapsed)
	log.Printf("[STATS] Total bytes sent: %d (%.2f MB)", totalBytesSent, float64(totalBytesSent)/(1024*1024))
	log.Printf("[STATS] Average transmission rate: %.2f Mbps", avgRate)

	return nil
}

func sendPacket(conn net.Conn, packetType uint8, seqNum, totalPackets uint32, data []byte) error {
	// Create header
	header := make([]byte, 13)
	header[0] = packetType
	binary.BigEndian.PutUint32(header[1:5], seqNum)
	binary.BigEndian.PutUint32(header[5:9], totalPackets)
	binary.BigEndian.PutUint32(header[9:13], uint32(len(data)))

	// Combine header and data
	packet := append(header, data...)

	// Send packet
	_, err := conn.Write(packet)
	return err
}
