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

	// Create RaptorQ processor
	processor, err := raptorq.NewDefaultRaptorQProcessor()
	if err != nil {
		return "", "", fmt.Errorf("failed to create RaptorQ processor: %v", err)
	}
	defer processor.Free()
	log.Printf("[SUCCESS] RaptorQ processor created")

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
		blockSize = int(processor.GetRecommendedBlockSize(uint64(fileInfo.Size())))
		log.Printf("[INFO] Using recommended block size: %d bytes (%.2f MB)", blockSize, float64(blockSize)/(1024*1024))
	} else {
		log.Printf("[INFO] Using specified block size: %d bytes", blockSize)
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

	// Get list of symbol files
	symbolFiles, err := filepath.Glob(filepath.Join(symbolsDir, "*"))
	if err != nil {
		return fmt.Errorf("failed to list symbol files: %v", err)
	}

	// Filter out the layout file
	var symbols []string
	for _, f := range symbolFiles {
		if filepath.Base(f) != "_raptorq_layout.json" {
			symbols = append(symbols, f)
		}
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

		seqNum := uint32(i + 1)
		if err := sendPacket(conn, 1, seqNum, totalPackets, data); err != nil {
			log.Printf("[WARNING] Failed to send symbol %d: %v", seqNum, err)
			continue
		}

		totalBytesSent += uint64(len(data) + 13)

		if config.VerboseLog {
			log.Printf("[PROGRESS] Sent symbol %d/%d (%s)", seqNum, len(symbols), filepath.Base(symbolFile))
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
