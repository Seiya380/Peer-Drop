package transfer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type Sender struct {
	selfID   string
	selfName string
	client   *http.Client
}

func NewSender(selfID, selfName string) *Sender {
	return &Sender{
		selfID:   selfID,
		selfName: selfName,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (s *Sender) SendFile(peerAddr string, peerPort int, filePath string) error {
	// Get file info
	stat, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	fileName := filepath.Base(filePath)
	mimeType := mime.TypeByExtension(filepath.Ext(filePath))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	// Step 1: Request transfer
	baseURL := fmt.Sprintf("http://%s:%d", peerAddr, peerPort)

	requestBody := TransferRequestBody{
		SenderID:   s.selfID,
		SenderName: s.selfName,
		Files: []FileInfo{
			{
				Name:     fileName,
				Size:     stat.Size(),
				MimeType: mimeType,
			},
		},
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := s.client.Post(baseURL+"/api/transfer/request", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("transfer request failed: %s", string(body))
	}

	var transferResp TransferResponse
	if err := json.NewDecoder(resp.Body).Decode(&transferResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	transferID := transferResp.TransferID

	// Step 2: Wait for acceptance
	fmt.Printf("Waiting for %s to accept transfer...\n", peerAddr)

	accepted := false
	for i := 0; i < 60; i++ { // 60 second timeout
		time.Sleep(1 * time.Second)

		statusResp, err := s.client.Get(fmt.Sprintf("%s/api/transfer/%s/status", baseURL, transferID))
		if err != nil {
			continue
		}

		var transfer Transfer
		if err := json.NewDecoder(statusResp.Body).Decode(&transfer); err != nil {
			statusResp.Body.Close()
			continue
		}
		statusResp.Body.Close()

		switch transfer.Status {
		case StatusAccepted:
			accepted = true
		case StatusRejected:
			return fmt.Errorf("transfer was rejected")
		case StatusCompleted:
			return nil
		case StatusFailed:
			return fmt.Errorf("transfer failed: %s", transfer.Error)
		}

		if accepted {
			break
		}
	}

	if !accepted {
		return fmt.Errorf("transfer request timed out")
	}

	// Step 3: Upload file
	fmt.Printf("Uploading %s...\n", fileName)

	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := io.Copy(part, file); err != nil {
		return fmt.Errorf("failed to copy file data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close writer: %w", err)
	}

	// Upload
	uploadReq, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/transfer/%s/upload", baseURL, transferID), body)
	if err != nil {
		return fmt.Errorf("failed to create upload request: %w", err)
	}
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())

	// Use a client with no timeout for large files
	uploadClient := &http.Client{Timeout: 0}
	uploadResp, err := uploadClient.Do(uploadReq)
	if err != nil {
		return fmt.Errorf("failed to upload: %w", err)
	}
	defer uploadResp.Body.Close()

	if uploadResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(uploadResp.Body)
		return fmt.Errorf("upload failed: %s", string(body))
	}

	fmt.Printf("File sent successfully!\n")
	return nil
}
