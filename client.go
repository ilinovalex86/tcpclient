package main

import (
	"bytes"
	"crypto/aes"
	"encoding/json"
	"errors"
	"fmt"
	cn "github.com/ilinovalex86/connection"
	ex "github.com/ilinovalex86/explorer"
	ie "github.com/ilinovalex86/inputevent"
	"github.com/ilinovalex86/screenshot"
	"image/jpeg"
	"log"
	"net"
	"os"
	"sync"
	"time"
)

const version = "0.1.1"
const key = "2665039024792361"

//Адрес и порт сервера
var tcpServer = "192.168.1.2:25001"
var streamServer = "192.168.1.2:25002"

//Структура клиента
type clientData struct {
	Sep      string
	BasePath string
	conn     net.Conn
	Id       string
	Version  string
	System   string
}

type stream struct {
	sync.Mutex
	statusChan chan bool
	imgChan    chan []byte
	conn       net.Conn
}

//Глобальные ошибки
var stopErrors = []string{
	"createId1",
	"createId2",
	"already exist",
	"downloadNewClient",
}

// Папка для скачивания файлов
var uploadDir = "files"

func init() {
	ie.InitIE()
	if !ex.ExistDir(uploadDir) {
		err := ex.MakeDir(uploadDir)
		if err != nil {
			log.Fatal(err)
		}
	}
}

func (cl *clientData) downloadNewClient(q cn.Query) error {
	var err error
	cn.SendSync(cl.conn)
	err = cn.GetFile(q.Query, q.DataLen, cl.conn)
	if err != nil {
		errors.New("downloadNewClient")
	}
	return nil
}

//Проверяет на критическую ошибку
func existStopErr(err string) bool {
	for _, v := range stopErrors {
		if v == err {
			return true
		}
	}
	return false
}

//Определяет тип ОС, имя пользователя, домашнюю папку, id.
func newClient() *clientData {
	fmt.Println("Инициализация")
	id := "new client"
	if ex.ExistFile("id.txt") {
		idByte, err := ex.ReadFileFull("id.txt")
		if err != nil {
			log.Fatal(err)
		}
		id = string(idByte)
	}
	cl := &clientData{
		Sep:      ex.Sep,
		BasePath: ex.BasePath,
		Id:       id,
		Version:  version,
		System:   ex.System,
	}
	fmt.Printf("BasePath: %12s \n", cl.BasePath)
	fmt.Printf("id: %8s \n", id)
	fmt.Printf("Version: %8s \n", version)
	return cl
}

//Обрабатывает подключение к серверу и передает данные о клиенте.
func (cl *clientData) connect() error {
	if !cl.validOnServer(cl.conn) {
		return errors.New("error valid")
	}
	jsonData, err := json.Marshal(cl)
	err = cn.SendBytesWithDelim(jsonData, cl.conn)
	if err != nil {
		return err
	}
	q, err := cn.ReadQuery(cl.conn)
	if err != nil {
		return err
	}
	switch q.Method {
	case "connect":
		return nil
	case "new id":
		err = cl.newId(q.Query)
		if err != nil {
			return err
		}
	case "already exist":
		return errors.New("already exist")
	case "downloadNewClient":
		err = cl.downloadNewClient(q)
		if err != nil {
			return err
		}
		log.Fatal("NewClient is downloaded")
	}
	return nil
}

//Проходит проверку на подключение к серверу
func (cl *clientData) validOnServer(conn net.Conn) bool {
	data, err := cn.ReadBytesByLen(16, conn)
	if err != nil {
		return false
	}
	bc, err := aes.NewCipher([]byte(key))
	if err != nil {
		log.Fatal(err)
	}
	var code = make([]byte, 16)
	bc.Decrypt(code, data)
	s := string(code)
	res := s[len(s)/2:] + s[:len(s)/2]
	bc.Encrypt(code, []byte(res))
	err = cn.SendBytes(code, conn)
	if err != nil {
		return false
	}
	mes, err := cn.ReadString(conn)
	if err != nil || mes != "ok" {
		return false
	}
	return true
}

//Получает новый id от сервера и сохраняет его
func (cl *clientData) newId(id string) error {
	file, err := os.Create("id.txt")
	if err != nil {
		return errors.New("createId1")
	}
	defer file.Close()
	_, err = file.WriteString(id)
	if err != nil {
		return errors.New("createId2")
	}
	cl.Id = id
	fmt.Println("New Id: ", id)
	return nil
}

//Обрабатывает запрос на содержимое папки
func (cl *clientData) dir(path string) error {
	if path == "" {
		path = cl.BasePath
	}
	if ex.ExistDir(path) {
		res, err := ex.Explorer(path)
		if err != nil {
			err = cn.SendResponse(cn.Response{Err: err}, cl.conn)
			if err != nil {
				return err
			}
			return nil
		}
		res["nav"] = ex.NavFunc(path)
		data, err := json.Marshal(res)
		if err != nil {
			err = cn.SendResponse(cn.Response{Err: err}, cl.conn)
			if err != nil {
				return err
			}
			return nil
		}
		err = cn.SendResponse(cn.Response{DataLen: len(data)}, cl.conn)
		if err != nil {
			return err
		}
		cn.ReadSync(cl.conn)
		err = cn.SendBytes(data, cl.conn)
		if err != nil {
			return err
		}
	} else {
		err := cn.SendResponse(cn.Response{Err: errors.New(path + " is not exist")}, cl.conn)
		if err != nil {
			return err
		}
	}
	return nil
}

//Обрабатывает запрос на отправку файла
func (cl *clientData) fileFromClient(path string) error {
	if ex.ExistFile(path) {
		file, _ := os.Stat(path)
		err := cn.SendResponse(cn.Response{DataLen: int(file.Size()), Response: file.Name()}, cl.conn)
		if err != nil {
			return err
		}
		cn.ReadSync(cl.conn)
		err = cn.SendFile(path, cl.conn)
		if err != nil {
			return err
		}
	} else {
		err := cn.SendResponse(cn.Response{Err: errors.New(path + " is not exist")}, cl.conn)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *stream) stream() {
	event := ""
	var err error
	for {
		var events []ie.Event
		if event == "yes" {
			jsonEvents, err := cn.ReadByteByDelim(s.conn)
			if err != nil {
				fmt.Println(err)
				break
			}
			err = json.Unmarshal(jsonEvents, &events)
			if err != nil {
				fmt.Println(err)
				break
			}
		} else {
			cn.ReadSync(s.conn)
		}
		ie.EventsRun(events)
		s.statusChan <- true
		if ok := <-s.statusChan; !ok {
			return
		}
		imgB := <-s.imgChan
		err = cn.SendResponse(cn.Response{DataLen: len(imgB)}, s.conn)
		if err != nil {
			fmt.Println(err)
			break
		}
		event, err = cn.ReadString(s.conn)
		if err != nil {
			fmt.Println(err)
			break
		}
		err = cn.SendBytes(imgB, s.conn)
		if err != nil {
			fmt.Println(err)
			break
		}
	}
	s.statusChan <- false
}

//Обрабатывает запрос на отправку файла
func (cl *clientData) stream(c chan error) {
	streamConn, err := net.Dial("tcp", streamServer)
	if err != nil {
		c <- errors.New("streamServer not found")
		return
	}
	defer streamConn.Close()
	if !cl.validOnServer(streamConn) {
		c <- errors.New("error valid stream server")
		return
	}
	jsonData, err := json.Marshal(cl)
	if err != nil {
		c <- errors.New("json.Marshal(cl)")
		return
	}
	err = cn.SendBytesWithDelim(jsonData, streamConn)
	if err != nil {
		c <- err
		return
	}
	con, err := screenshot.Connect()
	if err != nil {
		c <- err
		return
	}
	defer screenshot.Close(con)
	ss := screenshot.ScreenSize(con)
	jsonSS, err := json.Marshal(ss)
	if err != nil {
		c <- errors.New("json.Marshal(ss)")
		return
	}
	cn.ReadSync(streamConn)
	err = cn.SendBytesWithDelim(jsonSS, streamConn)
	if err != nil {
		c <- err
		return
	}
	c <- nil
	imgChan := make(chan []byte)
	statusChan := make(chan bool)
	s := &stream{statusChan: statusChan, imgChan: imgChan, conn: streamConn}
	go s.stream()
	for {
		img, err := screenshot.CaptureScreen(con)
		if err != nil {
			fmt.Println(err)
			break
		}
		buf := new(bytes.Buffer)
		err = jpeg.Encode(buf, img, &jpeg.Options{Quality: 96})
		if err != nil {
			fmt.Println(err)
			break
		}
		imgB := buf.Bytes()
		if ok := <-statusChan; !ok {
			fmt.Println("end screen1")
			return
		}
		statusChan <- true
		imgChan <- imgB
	}
	if ok := <-statusChan; !ok {
		fmt.Println("end screen1")
		return
	}
	statusChan <- false
	fmt.Println("end screen2")
}

func (cl *clientData) fileToClient(q cn.Query) error {
	switch ex.System {
	case "linux":
		uploadDir += "/" + q.Query
	case "windows":
		uploadDir += "\\" + q.Query
	}
	cn.SendSync(cl.conn)
	err := cn.GetFile(uploadDir, q.DataLen, cl.conn)
	if err != nil {
		err = cn.SendResponse(cn.Response{Err: err}, cl.conn)
		if err != nil {
			return err
		}
		return err
	}
	err = cn.SendResponse(cn.Response{}, cl.conn)
	if err != nil {
		return err
	}
	return nil
}

//Принимает сообщения от сервера и обрабатывает их.
func worker(cl *clientData) error {
	for {
		q, err := cn.ReadQuery(cl.conn)
		if err != nil {
			return err
		}
		fmt.Printf("%#v %s \n", q, time.Now().Format(time.StampNano))
		switch q.Method {
		case "testConnect":
			err = cn.SendResponse(cn.Response{}, cl.conn)
			if err != nil {
				return err
			}
		case "dir":
			err = cl.dir(q.Query)
			if err != nil {
				return err
			}
		case "fileFromClient":
			err = cl.fileFromClient(q.Query)
			if err != nil {
				return err
			}
		case "fileToClient":
			err = cl.fileToClient(q)
			if err != nil {
				return err
			}
		case "stream":
			channel := make(chan error)
			go cl.stream(channel)
			err = <-channel
			if err != nil {
				err = cn.SendResponse(cn.Response{Err: err}, cl.conn)
				if err != nil {
					return err
				}
			}
			err = cn.SendResponse(cn.Response{}, cl.conn)
			if err != nil {
				return err
			}
		default:
			return errors.New("something wrong")
		}
	}
}

//Подключается к серверу, выводит ошибки
func main() {
	cl := newClient()
	for {
		conn, err := net.Dial("tcp", tcpServer)
		if err != nil {
			fmt.Println("Server not found")
			time.Sleep(5 * time.Second)
			continue
		}
		cl.conn = conn
		err = cl.connect()
		if err != nil {
			fmt.Println(err)
			cl.conn.Close()
			if existStopErr(fmt.Sprint(err)) {
				break
			}
			continue
		}
		fmt.Println("Connected")
		err = worker(cl)
		if err != nil {
			fmt.Println(err)
			cl.conn.Close()
			if fmt.Sprint(err) == "something wrong" {
				time.Sleep(time.Second * 20)
			}
		}
	}
}
