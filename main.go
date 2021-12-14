package main

import (
	"log"
	"net"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/songgao/packets/ethernet"
	"github.com/songgao/water"
	"github.com/vishvananda/netlink"
)

const Address = "10.5.0.1/16"
const MTU = 1500
const Debug = false

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var tap *water.Interface
var clients = map[string]*websocket.Conn{}

func main() {
	var err error
	// create tap device
	tap, err = water.New(water.Config{
		DeviceType: water.TAP,
	})
	if err != nil {
		log.Println("create tap:", err)
		return
	}
	defer tap.Close()

	log.Println("created tap:", tap.Name())

	// get newly created tap device
	link, err := netlink.LinkByName(tap.Name())
	if err != nil {
		log.Println("get link:", err)
		return
	}

	// set address on that device
	addr, _ := netlink.ParseAddr(Address)
	err = netlink.AddrAdd(link, addr)
	if err != nil {
		log.Println("set addr:", err)
		return
	}

	// make device up
	err = netlink.LinkSetUp(link)
	if err != nil {
		log.Println("set link up:", err)
		return
	}

	go func() {
		var frame ethernet.Frame

		for {
			frame.Resize(MTU)
			n, err := tap.Read(frame)
			if err != nil {
				log.Println("tap read:", err)
				continue
			}
			frame = frame[:n]
			if Debug {
				log.Printf("%s -> %s\n", frame.Source(), frame.Destination())
			}

			if conn, ok := clients[frame.Destination().String()]; ok {
				err := conn.WriteMessage(websocket.BinaryMessage, frame)
				if err != nil {
					log.Println("write message:", err)
					continue
				}
			} else if frame.Destination().String() == "ff:ff:ff:ff:ff:ff" {
				for _, client := range clients {
					err := client.WriteMessage(websocket.BinaryMessage, frame)
					if err != nil {
						log.Println("write message:", err)
						continue
					}
				}
			} else {
				log.Printf("%#v\n", clients)
			}
		}
	}()

	http.HandleFunc("/", handle)
	log.Println("listening!")
	panic(http.ListenAndServe("0.0.0.0:8080", nil))
}

func handle(wr http.ResponseWriter, req *http.Request) {
	c, err := upgrader.Upgrade(wr, req, nil)
	if err != nil {
		log.Println("upgrade:", err)
		return
	}
	defer c.Close()

	log.Println("connected!", c.RemoteAddr().String())

	var addr net.HardwareAddr
	defer func() {
		if addr != nil {
			delete(clients, addr.String())
		}
	}()

	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Println("read message:", err)
			return
		}

		frame := ethernet.Frame(message)
		if Debug {
			log.Printf("%s -> %s\n", frame.Source(), frame.Destination())
		}
		if addr == nil {
			addr = frame.Source()
			clients[addr.String()] = c
		}

		_, err = tap.Write(frame)
		if err != nil {
			log.Println("write tap:", err)
			return
		}
	}
}
