package main

import (
	"encoding/binary"
	"encoding/json"
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

// Config holds the receiver configuration
type Config struct {
	Port          int
	OutputDir     string
	BufferSize    int
	Timeout       int
	MaxMemoryMB   int
	VerboseLog    bool
}

func main() {
	// Parse command line arguments
	config := Config{}
	flag.IntVar(&config.Port, "port", 9000, "UDP port to listen on")
	flag.StringVar(&config.OutputDir, "output", "./received", "Output directory for received files")
	flag.IntVar(&config.BufferSize, "buffer", 65536, "UDP receive buffer size in bytes")
	flag.IntVar(&config.Timeout, "timeout", 300, "Timeout in seconds for receiving data")
	flag.IntVar(&config.MaxMemoryMB, "memory", 512, "Max memory usage in MB")
	flag.BoolVar(&config.VerboseLog, "verbose", false, "Enable verbose logging")
	flag.Parse()

	log.Printf("[INIT] UDP Receiver starting...")
	log.Printf("[CONFIG] Port: %d", config.Port)
	log.Printf("[CONFIG] Output directory: %s", config.OutputDir)
	log.Printf("[CONFIG] Buffer size: %d bytes", config.BufferSize)
	log.Printf("[CONFIG] Timeout: %d seconds", config.Timeout)
	log.Printf("[CONFIG] Max memory: %d MB", config.MaxMemoryMB)
	log.Printf("[CONFIG] Verbose logging: %v", config.VerboseLog)

	// Create output directory
	if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
		log.Fatalf("[ERROR] Failed to create output directory: %v", err)
	}
	log.Printf("[SUCCESS] Output directory created/verified: %s", config.OutputDir)

	// Start listening
	addr := fmt.Sprintf(":%d", config.Port)
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		log.Fatalf("[ERROR] Failed to listen on %s: %v", addr, err)
	}
	defer conn.Close()

	log.Printf("[SUCCESS] UDP receiver listening on %s", addr)
	log.Printf("[WAITING] Waiting for incoming data...")

	// Receive loop
	if err := receiveData(conn, &config); err != nil {
		log.Fatalf("[ERROR] Reception failed: %v", err)
	}
}

func receiveData(conn net.PacketConn, config *Config) error {
	buffer := make([]byte, config.BufferSize)
	var metadata []byte
	symbolsReceived := 0
	packetsReceived := 0
	startTime := time.Now()

	symbolsDir := filepath.Join(config.OutputDir, "symbols")
	if err := os.MkdirAll(symbolsDir, 0755); err != nil {
		return fmt.Errorf("failed to create symbols directory: %v", err)
	}
	log.Printf("[INFO] Symbols directory created: %s", symbolsDir)

	// Set read deadline
	conn.SetReadDeadline(time.Now().Add(time.Duration(config.Timeout) * time.Second))

	for {
		// Read packet
		n, addr, err := conn.ReadFrom(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				if packetsReceived == 0 {
					return fmt.Errorf("timeout waiting for data")
				}
				log.Printf("[TIMEOUT] No more packets received, processing collected data...")
				break
			}
			return fmt.Errorf("read error: %v", err)
		}

		packetsReceived++

		// Reset deadline after each successful packet
		conn.SetReadDeadline(time.Now().Add(time.Duration(config.Timeout) * time.Second))

		if config.VerboseLog {
			log.Printf("[PACKET] Received packet %d from %s, size: %d bytes", packetsReceived, addr, n)
		} else if packetsReceived%100 == 0 {
			log.Printf("[PROGRESS] Received %d packets so far...", packetsReceived)
		}

		// Parse header
		if n < 13 { // Minimum header size
			log.Printf("[WARNING] Packet too small, skipping")
			continue
		}

		header := PacketHeader{
			PacketType:   buffer[0],
			SequenceNum:  binary.BigEndian.Uint32(buffer[1:5]),
			TotalPackets: binary.BigEndian.Uint32(buffer[5:9]),
			DataLen:      binary.BigEndian.Uint32(buffer[9:13]),
		}

		if config.VerboseLog {
			log.Printf("[HEADER] Type: %d, Seq: %d/%d, DataLen: %d",
				header.PacketType, header.SequenceNum, header.TotalPackets, header.DataLen)
		}

		// Extract payload
		payload := buffer[13:13+header.DataLen]

		switch header.PacketType {
		case 0: // Metadata
			metadata = make([]byte, len(payload))
			copy(metadata, payload)
			log.Printf("[METADATA] Received metadata packet, size: %d bytes", len(metadata))

			// Save metadata to file
			layoutPath := filepath.Join(symbolsDir, "_raptorq_layout.json")
			if err := os.WriteFile(layoutPath, metadata, 0644); err != nil {
				log.Printf("[ERROR] Failed to save metadata: %v", err)
			} else {
				log.Printf("[SUCCESS] Metadata saved to: %s", layoutPath)
			}

		case 1: // Symbol data
			// Extract filename from payload (first 2 bytes are length)
			if len(payload) < 2 {
				log.Printf("[WARNING] Symbol packet %d too small", header.SequenceNum)
				continue
			}
			filenameLen := uint16(payload[0])<<8 | uint16(payload[1])
			if len(payload) < int(2+filenameLen) {
				log.Printf("[WARNING] Symbol packet %d invalid filename length", header.SequenceNum)
				continue
			}

			filename := string(payload[2:2+filenameLen])
			symbolData := payload[2+filenameLen:]

			// Create full path including subdirectories
			symbolPath := filepath.Join(symbolsDir, filename)
			symbolDir := filepath.Dir(symbolPath)

			// Create directory if needed
			if err := os.MkdirAll(symbolDir, 0755); err != nil {
				log.Printf("[ERROR] Failed to create directory for symbol %d: %v", header.SequenceNum, err)
				continue
			}

			// Save symbol data
			if err := os.WriteFile(symbolPath, symbolData, 0644); err != nil {
				log.Printf("[ERROR] Failed to save symbol %d: %v", header.SequenceNum, err)
			} else {
				symbolsReceived++
				if config.VerboseLog {
					log.Printf("[SYMBOL] Saved symbol %d to: %s", header.SequenceNum, filename)
				}
			}

		case 2: // End of transmission
			log.Printf("[EOT] End of transmission received")
			log.Printf("[STATS] Total packets received: %d", packetsReceived)
			log.Printf("[STATS] Total symbols received: %d", symbolsReceived)
			elapsed := time.Since(startTime)
			log.Printf("[STATS] Total reception time: %v", elapsed)

			// Process the received data
			if metadata == nil {
				return fmt.Errorf("no metadata received")
			}

			return decodeReceivedData(symbolsDir, config.OutputDir, metadata, config.MaxMemoryMB)
		}
	}

	// If we reach here, timeout occurred but we have data
	log.Printf("[INFO] Timeout reached with %d packets and %d symbols", packetsReceived, symbolsReceived)
	if metadata != nil && symbolsReceived > 0 {
		elapsed := time.Since(startTime)
		log.Printf("[STATS] Total reception time: %v", elapsed)
		return decodeReceivedData(symbolsDir, config.OutputDir, metadata, config.MaxMemoryMB)
	}

	return fmt.Errorf("incomplete transmission: no valid data received")
}

func decodeReceivedData(symbolsDir, outputDir string, metadataJSON []byte, maxMemoryMB int) error {
	log.Printf("[DECODE] Starting decode process...")
	log.Printf("[DECODE] Symbols directory: %s", symbolsDir)

	// Parse metadata to get original filename
	var layoutData struct {
		Blocks []struct {
			BlockID int `json:"block_id"`
		} `json:"blocks"`
	}

	if err := json.Unmarshal(metadataJSON, &layoutData); err != nil {
		log.Printf("[WARNING] Failed to parse metadata for filename: %v", err)
	}

	// Create RaptorQ processor with matching configuration
	// Use 1400 bytes to match sender's symbol size
	processor, err := raptorq.NewRaptorQProcessor(
		1400,                   // Symbol size: 1400 bytes (matching sender)
		4,                      // Redundancy factor
		uint64(maxMemoryMB),   // Max memory from config
		4,                      // Concurrency limit
	)
	if err != nil {
		return fmt.Errorf("failed to create RaptorQ processor: %v", err)
	}
	defer processor.Free()
	log.Printf("[SUCCESS] RaptorQ processor created with 1400-byte symbols, %dMB memory limit", maxMemoryMB)

	// Prepare paths
	layoutPath := filepath.Join(symbolsDir, "_raptorq_layout.json")
	outputPath := filepath.Join(outputDir, "decoded_file")

	log.Printf("[DECODE] Layout file: %s", layoutPath)
	log.Printf("[DECODE] Output file: %s", outputPath)

	// Decode symbols
	log.Printf("[DECODE] Decoding symbols back to original file...")
	if err := processor.DecodeSymbols(symbolsDir, outputPath, layoutPath); err != nil {
		return fmt.Errorf("decoding failed: %v", err)
	}

	log.Printf("[SUCCESS] File decoded successfully!")
	log.Printf("[SUCCESS] Decoded file saved to: %s", outputPath)

	// Get file size
	if info, err := os.Stat(outputPath); err == nil {
		log.Printf("[INFO] Decoded file size: %d bytes", info.Size())
	}

	return nil
}
