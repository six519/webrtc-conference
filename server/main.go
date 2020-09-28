package main

import (
    "html/template"
    "net/http"
    "fmt"
    "github.com/gorilla/websocket"
    "encoding/json"
    "github.com/google/uuid"
    "github.com/pion/webrtc/v2"
    "sync"
)

type JSONMessage struct {
    Command string		`json:"command"`
    CurrentID string	`json:"current_id"`
    Data string			`json:"data"`
}

var (
    SERVER_PORT = "8000"
    websocketUpgrader = websocket.Upgrader{}
    connections = make(map[*websocket.Conn]bool)
    mediaEngine webrtc.MediaEngine
    webrtcApi *webrtc.API
    //I don't fucking care if you use my turn/stun test credentials!!!
    connectionConfig = webrtc.Configuration{
        ICEServers: []webrtc.ICEServer{
            {
                URLs: []string{"stun:ss-turn1.xirsys.com"},
            },
            {
                URLs: []string{"turn:ss-turn1.xirsys.com:80?transport=udp", "turn:ss-turn1.xirsys.com:3478?transport=udp", "turn:ss-turn1.xirsys.com:80?transport=tcp", "turn:ss-turn1.xirsys.com:3478?transport=tcp", "turns:ss-turn1.xirsys.com:443?transport=tcp", "turns:ss-turn1.xirsys.com:5349?transport=tcp"},
                Username:       "nDc_obV6zSypEKQTiDtEca5CA6vYZLasLAjIf9VwVXBc54FFpQbv5PvUwili43_0AAAAAF9Jf8FzaXg1MTk=",
                Credential:     "a3bd4a9a-e97a-11ea-9bbd-0242ac140004",
                CredentialType: webrtc.ICECredentialTypePassword,
            },
        },
        SDPSemantics: webrtc.SDPSemanticsUnifiedPlanWithFallback,
    }
    videoReceivers = make(map[string]*webrtc.PeerConnection)
    videoSenders = make(map[string]*webrtc.PeerConnection)

    videoTrackLocks = make(map[string]sync.RWMutex)
    audioTrackLocks = make(map[string]sync.RWMutex)
)

func showError(err error) {
    if err != nil {
        panic(err)
    }
}

func web(writer http.ResponseWriter, request *http.Request) {
    if request.Method == "GET" {
        temp, _ := template.ParseFiles("../index.html")
        showError(temp.Execute(writer, nil))
    }
}

func ws(writer http.ResponseWriter, request *http.Request) {
    connection, err := websocketUpgrader.Upgrade(writer, request, nil)
    showError(err)
    defer func() {
        showError(connection.Close())
        delete(connections, connection)
    }()

    connections[connection] = true

    messageType, jsonMessage, err := connection.ReadMessage()
    showError(err)

    fmt.Println("The message is:" + string(jsonMessage))

    msg := JSONMessage{}
    json.Unmarshal(jsonMessage, &msg)

    if (msg.Command == "init" && msg.CurrentID == "") {
        thisId := uuid.New().String()
        reply := JSONMessage{
            Command: "init",
            CurrentID: thisId,
        }

        replyMsg, err := json.Marshal(&reply)
        showError(err)
        showError(connection.WriteMessage(messageType, []byte(replyMsg)))
    } else if (msg.Command == "send_sdp") {
        videoReceivers[msg.CurrentID], err = webrtcApi.NewPeerConnection(connectionConfig)
        videoTrackLocks[msg.CurrentID] = sync.RWMutex{}
        audioTrackLocks[msg.CurrentID] = sync.RWMutex{}
        showError(err)

        _, err = videoReceivers[msg.CurrentID].AddTransceiver(webrtc.RTPCodecTypeAudio)
        showError(err)

        _, err = videoReceivers[msg.CurrentID].AddTransceiver(webrtc.RTPCodecTypeVideo)
        showError(err)

        videoReceivers[msg.CurrentID].OnTrack(func(track *webrtc.Track, rtpReceiver *webrtc.RTPReceiver) {

        })
    }
}

func main() {

    mediaEngine = webrtc.MediaEngine{}

    //supported video codec
    mediaEngine.RegisterCodec(webrtc.NewRTPVP8Codec(webrtc.DefaultPayloadTypeVP8, 90000))
    //supported audio codec
    mediaEngine.RegisterCodec(webrtc.NewRTPOpusCodec(webrtc.DefaultPayloadTypeOpus, 48000))
    webrtcApi = webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))

    http.HandleFunc("/", web)
    http.HandleFunc("/ws", ws)

    fmt.Println("Listening at port: " + SERVER_PORT)
    showError(http.ListenAndServe(":" + SERVER_PORT, nil))
}