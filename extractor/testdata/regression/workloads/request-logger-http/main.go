package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Todo struct {
	ID        int    `json:"id"`
	Title     string `json:"title"`
	Completed bool   `json:"completed"`
}

func init() {
	declaration()
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/ready", readinessHandler)
	mux.HandleFunc("/demo", demoHandler)

	addr := ":" + envOrDefault("PORT", "8080")
	log.Printf("request-logger demo listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func readinessHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := checkTodosAPI(ctx); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	if err := checkRedis(ctx); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func demoHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	result := map[string]string{
		"todosApi": statusString(checkTodosAPI(ctx)),
		"cache":    statusString(checkRedis(ctx)),
	}

	status := http.StatusOK
	if result["todosApi"] != "ok" || result["cache"] != "ok" {
		status = http.StatusInternalServerError
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if status == http.StatusInternalServerError {
		w.Write([]byte(fmt.Sprintf("One or more dependencies are not healthy: todosApi=%s, cache=%s", result["todosApi"], result["cache"])))
	}
	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func checkTodosAPI(ctx context.Context) error {
	baseURL := strings.TrimRight(os.Getenv("TODOS_API_URL"), "/")
	if baseURL == "" {
		return errors.New("TODOS_API_URL is not set")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/todos/1", nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("todos-api request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("todos-api returned %s", response.Status)
	}

	var todo Todo
	if err := json.NewDecoder(response.Body).Decode(&todo); err != nil {
		return fmt.Errorf("todos-api response decode failed: %w", err)
	}
	if todo.ID == 0 || todo.Title == "" {
		return errors.New("todos-api response was incomplete")
	}
	return nil
}

func checkRedis(ctx context.Context) error {
	addr, err := redisAddress()
	if err != nil {
		return err
	}

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("redis dial failed: %w", err)
	}
	defer conn.Close()

	deadline, ok := ctx.Deadline()
	if ok {
		_ = conn.SetDeadline(deadline)
	}
	if _, err := conn.Write([]byte("*1\r\n$4\r\nPING\r\n")); err != nil {
		return fmt.Errorf("redis ping write failed: %w", err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return fmt.Errorf("redis ping read failed: %w", err)
	}
	if !strings.HasPrefix(line, "+PONG") {
		return fmt.Errorf("redis ping returned %q", strings.TrimSpace(line))
	}
	return nil
}

func redisAddress() (string, error) {
	if rawURL := os.Getenv("REDIS_URL"); rawURL != "" {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return "", fmt.Errorf("invalid REDIS_URL: %w", err)
		}
		if parsed.Host != "" {
			return parsed.Host, nil
		}
	}

	host := os.Getenv("REDIS_HOST")
	port := envOrDefault("REDIS_PORT", "6379")
	if host == "" {
		return "", errors.New("REDIS_URL or REDIS_HOST must be set")
	}
	return net.JoinHostPort(host, port), nil
}

func statusString(err error) string {
	if err != nil {
		log.Print(err)
		return "error"
	}
	return "ok"
}

func envOrDefault(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
