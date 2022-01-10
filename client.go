package main

import (
	"encoding/json"
	"errors"
	"fmt"
	cn "github.com/ilinovalex86/connection"
	ex "github.com/ilinovalex86/explorer"
	"net"
	"os"
	"time"
)

//Адрес и порт сервера
var tcpServer = "ipAddress:port"

//Структура клиента
type clientData struct {
	system   string
	Sep      string
	User     string
	BasePath string
	conn     net.Conn
	Id       string
}

//Глобальные ошибки
var stopErrors = []string{
	"createId1",
	"createId2",
	"already exist",
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
		id = string(ex.ReadFileFull("id.txt"))
	}
	cl := &clientData{
		system:   ex.System,
		Sep:      ex.Sep,
		User:     ex.User,
		BasePath: ex.BasePath,
		Id:       id,
	}
	fmt.Printf("System: %9s \n", cl.system)
	fmt.Printf("User: %10s \n", cl.User)
	fmt.Printf("BasePath: %12s \n", cl.BasePath)
	fmt.Printf("id: %8s \n", id)
	return cl
}

//Обрабатывает подключение к серверу и передает данные о клиенте.
func (cl *clientData) connect() error {
	if !cl.validOnServer() {
		return errors.New("error valid")
	}
	jsonData, err := json.Marshal(cl)
	err = cn.SendBytesWithDelim(jsonData, cl.conn)
	if err != nil {
		return err
	}
	q, err := cn.ReadQRStruct(cl.conn)
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
	}
	return nil
}

//Проходит проверку на подключение к серверу
func (cl *clientData) validOnServer() bool {
	s, err := cn.ReadString(cl.conn)
	if err != nil {
		return false
	}
	err = cn.SendString(s[len(s)/2:]+s[:len(s)/2], cl.conn)
	if err != nil {
		return false
	}
	mes, err := cn.ReadString(cl.conn)
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
	return nil
}

//Обрабатывает запрос на содержимое папки
func (cl *clientData) dir(path string) error {
	if ex.ExistDir(path) {
		res, err := ex.Explorer(path)
		if err != nil {
			return err
		}
		res["nav"] = ex.NavFunc(path)
		data, err := json.Marshal(res)
		if err != nil {
			return err
		}
		jS := cn.QueryResponse{DataLen: len(data)}
		err = cn.SendQRStruct(jS, cl.conn)
		if err != nil {
			return err
		}
		time.Sleep(time.Millisecond)
		err = cn.SendBytes(data, cl.conn)
		if err != nil {
			return err
		}
	} else {
		jS := cn.QueryResponse{Err: errors.New(path + " is not exist")}
		err := cn.SendQRStruct(jS, cl.conn)
		if err != nil {
			return err
		}
	}
	return nil
}

//Обрабатывает запрос на отправку файла
func (cl *clientData) file(path string) error {
	if ex.ExistFile(path) {
		file, _ := os.Stat(path)
		jS := cn.QueryResponse{DataLen: int(file.Size()), FileName: file.Name()}
		err := cn.SendQRStruct(jS, cl.conn)
		if err != nil {
			return err
		}
		time.Sleep(time.Millisecond)
		err = cn.SendFile(path, cl.conn)
		if err != nil {
			return err
		}
	} else {
		jS := cn.QueryResponse{Err: errors.New(path + " is not exist")}
		err := cn.SendQRStruct(jS, cl.conn)
		if err != nil {
			return err
		}
	}
	return nil
}

//Принимает сообщения от сервера и обрабатывает их.
func worker(cl *clientData) error {
	for {
		q, err := cn.ReadQRStruct(cl.conn)
		if err != nil {
			return err
		}
		switch q.Method {
		case "testConnect":
			err = cn.SendQRStruct(cn.QueryResponse{}, cl.conn)
			if err != nil {
				return err
			}
		case "dir":
			err = cl.dir(q.Query)
			if err != nil {
				return err
			}
		case "file":
			err = cl.file(q.Query)
			if err != nil {
				return err
			}
		default:
			fmt.Printf("%#v \n", q)
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
