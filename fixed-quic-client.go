package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/quic-go/quic-go"
)

const (
	uploadDir   = "./client_files"
	downloadDir = "./client_downloads"
)

func main() {
	// 디렉토리 생성
	if err := ensureDir(uploadDir); err != nil {
		log.Fatalf("업로드 디렉토리 생성 실패: %v", err)
	}
	if err := ensureDir(downloadDir); err != nil {
		log.Fatalf("다운로드 디렉토리 생성 실패: %v", err)
	}

	// 예시 파일 생성
	createExampleFiles()

	// 기본 TLS 설정 (QUIC에 필요)
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-example"},
	}

	for {
		fmt.Println("\nQUIC 클라이언트 메뉴:")
		fmt.Println("1. 서버에 파일 업로드")
		fmt.Println("2. 서버에서 파일 다운로드")
		fmt.Println("3. 서버의 파일 목록 확인")
		fmt.Println("4. 종료")
		fmt.Print("선택하세요: ")

		var choice int
		fmt.Scanln(&choice)

		switch choice {
		case 1:
			uploadFile(tlsConfig)
		case 2:
			downloadFile(tlsConfig)
		case 3:
			listFiles(tlsConfig)
		case 4:
			fmt.Println("프로그램을 종료합니다.")
			return
		default:
			fmt.Println("잘못된 선택입니다. 다시 시도하세요.")
		}
	}
}

func uploadFile(tlsConfig *tls.Config) {
	// 업로드할 파일 선택
	fmt.Println("\n업로드할 파일:")
	files, err := os.ReadDir(uploadDir)
	if err != nil {
		log.Printf("디렉토리 읽기 오류: %v", err)
		return
	}

	if len(files) == 0 {
		fmt.Println("업로드할 파일이 없습니다.")
		return
	}

	fileList := []os.DirEntry{}
	for _, file := range files {
		if !file.IsDir() {
			fileList = append(fileList, file)
		}
	}

	if len(fileList) == 0 {
		fmt.Println("업로드할 파일이 없습니다.")
		return
	}

	for i, file := range fileList {
		fileInfo, _ := file.Info()
		fmt.Printf("%d. %s (%d 바이트)\n", i+1, file.Name(), fileInfo.Size())
	}

	fmt.Print("파일 번호를 선택하세요: ")
	var fileIndex int
	fmt.Scanln(&fileIndex)
	if fileIndex < 1 || fileIndex > len(fileList) {
		fmt.Println("잘못된 파일 번호입니다.")
		return
	}

	fileName := fileList[fileIndex-1].Name()
	filePath := filepath.Join(uploadDir, fileName)

	// 서버 연결
	conn, err := connectToServer(tlsConfig)
	if err != nil {
		log.Printf("서버 연결 실패: %v", err)
		return
	}
	defer conn.CloseWithError(0, "클라이언트 종료")

	// 스트림 열기
	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		log.Printf("스트림 열기 실패: %v", err)
		return
	}
	defer stream.Close()

	// 파일 열기
	file, err := os.Open(filePath)
	if err != nil {
		log.Printf("파일 열기 오류: %v", err)
		return
	}
	defer file.Close()

	// 명령어 전송 (10바이트로 패딩)
	cmd := make([]byte, 10)
	copy(cmd, []byte("UP"))
	_, err = stream.Write(cmd)
	if err != nil {
		log.Printf("명령어 전송 오류: %v", err)
		return
	}

	// 파일명 전송
	_, err = stream.Write([]byte(fileName))
	if err != nil {
		log.Printf("파일명 전송 오류: %v", err)
		return
	}

	// 파일 내용 전송 - stream을 io.Writer로 캐스팅
	n, err := io.Copy(io.Writer(stream), file)
	if err != nil {
		log.Printf("파일 전송 오류: %v", err)
		return
	}

	// 스트림 닫기 알림
	stream.Close()

	// 응답 대기를 위한 새 스트림 (선택적)
	fmt.Printf("'%s' 파일 업로드 완료: %d 바이트 전송\n", fileName, n)
}

func downloadFile(tlsConfig *tls.Config) {
	// 서버 연결
	conn, err := connectToServer(tlsConfig)
	if err != nil {
		log.Printf("서버 연결 실패: %v", err)
		return
	}
	defer conn.CloseWithError(0, "클라이언트 종료")

	// 먼저 파일 목록 가져오기
	fileList := getFileList(conn)
	if fileList == "" {
		fmt.Println("파일 목록을 가져오는데 실패했습니다.")
		return
	}

	// 파일 목록 표시
	files := strings.Split(strings.TrimSpace(fileList), "\n")
	if len(files) == 0 || (len(files) == 1 && files[0] == "") {
		fmt.Println("서버에 다운로드할 파일이 없습니다.")
		return
	}

	fmt.Println("\n다운로드할 파일:")
	for i, file := range files {
		fmt.Printf("%d. %s\n", i+1, file)
	}

	// 파일 선택
	fmt.Print("파일 번호를 선택하세요: ")
	var fileIndex int
	fmt.Scanln(&fileIndex)
	if fileIndex < 1 || fileIndex > len(files) {
		fmt.Println("잘못된 파일 번호입니다.")
		return
	}

	// 파일명만 추출 (크기 정보 제외)
	selectedFile := files[fileIndex-1]
	fileName := strings.Split(selectedFile, " (")[0]

	// 다운로드 스트림 열기
	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		log.Printf("스트림 열기 실패: %v", err)
		return
	}
	defer stream.Close()

	// 명령어 전송 (10바이트로 패딩)
	cmd := make([]byte, 10)
	copy(cmd, []byte("DOWN"))
	_, err = stream.Write(cmd)
	if err != nil {
		log.Printf("명령어 전송 오류: %v", err)
		return
	}

	// 파일명 전송
	_, err = stream.Write([]byte(fileName))
	if err != nil {
		log.Printf("파일명 전송 오류: %v", err)
		return
	}

	// 파일 크기 읽기
	sizeBytes := make([]byte, 20)
	n, err := stream.Read(sizeBytes)
	if err != nil {
		log.Printf("파일 크기 읽기 오류: %v", err)
		return
	}

	sizeStr := strings.TrimSpace(string(sizeBytes[:n]))
	if strings.Contains(sizeStr, "ERROR") {
		fmt.Printf("'%s' 파일을 찾을 수 없습니다.\n", fileName)
		return
	}

	fileSize, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil {
		log.Printf("파일 크기 변환 오류: %v, 받은 값: '%s'", err, sizeStr)
		return
	}

	// 준비 완료 메시지 전송
	_, err = stream.Write([]byte("READY"))
	if err != nil {
		log.Printf("준비 메시지 전송 오류: %v", err)
		return
	}

	// 파일 생성
	filePath := filepath.Join(downloadDir, fileName)
	file, err := os.Create(filePath)
	if err != nil {
		log.Printf("파일 생성 오류: %v", err)
		return
	}
	defer file.Close()

	// 파일 내용 수신 - stream을 io.Reader로 캐스팅
	received, err := io.Copy(file, io.Reader(stream))
	if err != nil && err != io.EOF {
		log.Printf("파일 수신 오류: %v", err)
		return
	}

	if received == fileSize {
		fmt.Printf("'%s' 파일 다운로드 성공: %d 바이트\n", fileName, received)
	} else {
		fmt.Printf("'%s' 파일 다운로드 완료: %d/%d 바이트\n", fileName, received, fileSize)
	}
}

func listFiles(tlsConfig *tls.Config) {
	// 서버 연결
	conn, err := connectToServer(tlsConfig)
	if err != nil {
		log.Printf("서버 연결 실패: %v", err)
		return
	}
	defer conn.CloseWithError(0, "클라이언트 종료")

	fileList := getFileList(conn)
	if fileList == "" {
		fmt.Println("파일 목록을 가져오는데 실패했습니다.")
		return
	}

	fmt.Println("\n서버의 파일 목록:")
	if strings.TrimSpace(fileList) == "" {
		fmt.Println("(비어있음)")
	} else {
		fmt.Println(fileList)
	}
}

// 파일 목록을 가져오는 함수
func getFileList(conn quic.Connection) string {
	// 스트림 열기
	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		log.Printf("스트림 열기 실패: %v", err)
		return ""
	}
	defer stream.Close()

	// 명령어 전송 (10바이트로 패딩)
	cmd := make([]byte, 10)
	copy(cmd, []byte("LIST"))
	_, err = stream.Write(cmd)
	if err != nil {
		log.Printf("명령어 전송 오류: %v", err)
		return ""
	}

	// 파일 목록 읽기
	listBytes, err := io.ReadAll(io.Reader(stream))
	if err != nil && err != io.EOF {
		log.Printf("목록 읽기 오류: %v", err)
		return ""
	}

	return string(listBytes)
}

// 서버 연결 함수
func connectToServer(tlsConfig *tls.Config) (quic.Connection, error) {
	conn, err := quic.DialAddr(context.Background(), "localhost:4242", tlsConfig, nil)
	if err != nil {
		return nil, fmt.Errorf("서버 연결 실패: %v", err)
	}
	fmt.Println("서버에 연결되었습니다.")
	return conn, nil
}

// 디렉토리가 존재하는지 확인하고 없으면 생성
func ensureDir(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return os.MkdirAll(dir, 0755)
	}
	return nil
}

// 예시 파일 생성
func createExampleFiles() {
	files := map[string]string{
		"upload1.txt": "클라이언트에서 서버로 업로드할 테스트 파일 1입니다.",
		"upload2.txt": "클라이언트에서 서버로 업로드할 테스트 파일 2입니다.\n여러 줄로 구성됩니다.\n파일 전송 테스트에 사용됩니다.",
		"upload3.txt": "클라이언트에서 서버로 업로드할 테스트 파일 3입니다.",
	}

	for name, content := range files {
		filePath := filepath.Join(uploadDir, name)
		// 파일이 이미 존재하지 않을 때만 생성
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				log.Printf("예시 파일 생성 실패 %s: %v", name, err)
			}
		}
	}
}