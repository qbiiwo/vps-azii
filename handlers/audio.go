package handlers

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"time"
)

const maxAudioSize int64 = 25 * 1024 * 1024
const maxStoredAudioSize int64 = 100 * 1024 * 1024

const whisperURL = "http://127.0.0.1:8081/inference"

var whisperClient = &http.Client{
	Timeout: 10 * time.Minute,
}

// Одновременно отправляем Whisper только одну задачу.
var transcriptionSlots = make(chan struct{}, 1)

var audioMemory = struct {
	sync.RWMutex
	files       map[string][]byte
	transcripts map[string]string
	size        int64
}{
	files:       make(map[string][]byte),
	transcripts: make(map[string]string),
}

type AudioResponse struct {
	Status    string `json:"status"`
	ID        string `json:"id"`
	SizeBytes int    `json:"size_bytes"`
	Storage   string `json:"storage"`
}

func UploadAudio(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAudioSize)

	audio, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError

		if errors.As(err, &maxErr) {
			http.Error(w, "Audio too large", http.StatusRequestEntityTooLarge)
			return
		}

		http.Error(w, "Cannot read audio", http.StatusBadRequest)
		return
	}

	if len(audio) == 0 {
		http.Error(w, "Empty audio", http.StatusBadRequest)
		return
	}

	// Генерируем уникальный ID записи.
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		clear(audio)
		http.Error(w, "Cannot generate ID", http.StatusInternalServerError)
		return
	}

	id := hex.EncodeToString(idBytes)

	// Сохраняем оригинальное аудио в RAM.
	audioMemory.Lock()

	if audioMemory.size+int64(len(audio)) > maxStoredAudioSize {
		audioMemory.Unlock()
		clear(audio)
		http.Error(w, "RAM storage limit reached", http.StatusInsufficientStorage)
		return
	}

	audioMemory.files[id] = audio
	audioMemory.size += int64(len(audio))
	currentSize := audioMemory.size

	audioMemory.Unlock()

	log.Println("================================")
	log.Printf("[AUDIO] Received: %d bytes", len(audio))
	log.Printf("[AUDIO] ID: %s", id)
	log.Printf("[RAM] Stored: %d bytes", currentSize)
	log.Println("[RAM] Audio retained in memory")
	log.Println("[WHISPER] Transcription queued")
	log.Println("================================")

	// Запускаем распознавание в фоне.
	go transcribeAudio(id, audio)

	// Android сразу получает подтверждение приёма.
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	response := AudioResponse{
		Status:    "stored",
		ID:        id,
		SizeBytes: len(audio),
		Storage:   "ram",
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}

func transcribeAudio(id string, audio []byte) {
	// Не допускаем одновременную обработку нескольких аудио.
	transcriptionSlots <- struct{}{}
	defer func() {
		<-transcriptionSlots
	}()

	log.Printf("[WHISPER] Starting transcription: %s", id)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		log.Printf("[WHISPER] Cannot create audio field: %v", err)
		return
	}

	if _, err := part.Write(audio); err != nil {
		log.Printf("[WHISPER] Cannot write audio: %v", err)
		return
	}

	if err := writer.WriteField("response_format", "json"); err != nil {
		log.Printf("[WHISPER] Cannot set response format: %v", err)
		return
	}

	if err := writer.Close(); err != nil {
		log.Printf("[WHISPER] Cannot finish request: %v", err)
		return
	}

	req, err := http.NewRequest(
		http.MethodPost,
		whisperURL,
		&body,
	)
	if err != nil {
		log.Printf("[WHISPER] Cannot create request: %v", err)
		return
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := whisperClient.Do(req)
	if err != nil {
		log.Printf("[WHISPER] Request failed: %v", err)
		return
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		log.Printf("[WHISPER] Cannot read response: %v", err)
		return
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf(
			"[WHISPER] HTTP %d: %s",
			resp.StatusCode,
			string(responseBody),
		)
		return
	}

	var result struct {
		Text string `json:"text"`
	}

	if err := json.Unmarshal(responseBody, &result); err != nil {
		log.Printf("[WHISPER] Invalid JSON response: %v", err)
		return
	}

	transcript := strings.TrimSpace(result.Text)

	// Сохраняем распознанный текст в RAM под тем же ID.
	audioMemory.Lock()
	audioMemory.transcripts[id] = transcript
	audioMemory.Unlock()

	log.Println("================================")
	log.Printf("[WHISPER] Transcription completed: %s", id)
	log.Println("[TRANSCRIPT]")
	fmt.Println(transcript)
	log.Println("================================")
}
