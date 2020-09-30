/*
    Thanks to the sample code: https://github.com/pion/example-webrtc-applications/tree/master/sfu-ws
*/
package main

import (
    "html/template"
    "net/http"
    "fmt"
    "github.com/gorilla/websocket"
    "encoding/json"
    "github.com/google/uuid"
    "github.com/pion/webrtc/v2"
    "github.com/pion/rtcp"
    "sync"
    "time"
    "io"
)

type JSONMessage struct {
    Command string      `json:"command"`
    CurrentID string    `json:"current_id"`
    Data string         `json:"data"`
}

const (
    rtcpPLIInterval = time.Second * 3
    SERVER_PORT = "3000"
)

var (
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
                Username: "nDc_obV6zSypEKQTiDtEca5CA6vYZLasLAjIf9VwVXBc54FFpQbv5PvUwili43_0AAAAAF9Jf8FzaXg1MTk=",
                Credential: "a3bd4a9a-e97a-11ea-9bbd-0242ac140004",
                CredentialType: webrtc.ICECredentialTypePassword,
            },
        },
        SDPSemantics: webrtc.SDPSemanticsUnifiedPlanWithFallback,
    }
    videoReceivers = make(map[string]*webrtc.PeerConnection)
    //videoSenders = make(map[string]*webrtc.PeerConnection)

    videoTrackLocks = make(map[string]sync.RWMutex)
    audioTrackLocks = make(map[string]sync.RWMutex)
    videoTracks = make(map[string]*webrtc.Track)
    audioTracks = make(map[string]*webrtc.Track)
)

func showError(err error) {
    if err != nil {
        panic(err)
    }
}

func broadcastMessage(reply JSONMessage, messageType int) {
    replyMsg, err := json.Marshal(&reply)
    showError(err)

    for connection := range connections {
        showError(connection.WriteMessage(messageType, []byte(replyMsg)))
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

    //fmt.Println("The message is:" + string(jsonMessage))

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

        videoReceivers[msg.CurrentID].OnConnectionStateChange(func(currentState webrtc.PeerConnectionState){
            if (currentState == webrtc.PeerConnectionStateDisconnected) {
                fmt.Println("Cleaning up...")
                //cleaning up shits
                delete(audioTracks, msg.CurrentID)
                delete(videoTracks, msg.CurrentID)
                videoReceivers[msg.CurrentID].Close()
                delete(videoReceivers, msg.CurrentID)
                delete(videoTrackLocks, msg.CurrentID)
                delete(audioTrackLocks, msg.CurrentID)

                reply := JSONMessage{
                    Command: "disconnected",
                    CurrentID: msg.CurrentID,
                }
                broadcastMessage(reply, messageType)
            }  
        })

        videoReceivers[msg.CurrentID].OnTrack(func(track *webrtc.Track, rtpReceiver *webrtc.RTPReceiver) {
            if track.PayloadType() == webrtc.DefaultPayloadTypeVP8 || track.PayloadType() == webrtc.DefaultPayloadTypeVP9 || track.PayloadType() == webrtc.DefaultPayloadTypeH264 {
                var err error
                vl := videoTrackLocks[msg.CurrentID]
                vl.Lock()
                videoTracks[msg.CurrentID], err = videoReceivers[msg.CurrentID].NewTrack(track.PayloadType(), track.SSRC(), "video", "pion")
                vl.Unlock()
                showError(err)

                go func() {
                    ticker := time.NewTicker(rtcpPLIInterval)
                    for range ticker.C {
                        _, ok := videoReceivers[msg.CurrentID]
                        if (!ok) {
                            break
                        }
                        err := videoReceivers[msg.CurrentID].WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: videoTracks[msg.CurrentID].SSRC()}})

                        if err != nil {
                            break
                        }
                    }
                }()

                rtpBuf := make([]byte, 1400)
                for {
                    i, err := track.Read(rtpBuf)
                    if (err != nil) {
                        break
                    }
                    vl.RLock()
                    _, err = videoTracks[msg.CurrentID].Write(rtpBuf[:i])
                    vl.RUnlock()

                    if err != io.ErrClosedPipe {
                        showError(err)
                    }
                }
            } else {
                var err error
                al := audioTrackLocks[msg.CurrentID]
                al.Lock()
                audioTracks[msg.CurrentID], err = videoReceivers[msg.CurrentID].NewTrack(track.PayloadType(), track.SSRC(), "audio", "pion")
                al.Unlock()
                showError(err)

                rtpBuf := make([]byte, 1400)
                for {
                    i, err := track.Read(rtpBuf)
                    if (err != nil) {
                        break
                    }
                    al.RLock()
                    _, err = audioTracks[msg.CurrentID].Write(rtpBuf[:i])
                    al.RUnlock()
                    if err != io.ErrClosedPipe {
                        showError(err)
                    }
                }
            }
        })

        showError(videoReceivers[msg.CurrentID].SetRemoteDescription(
        webrtc.SessionDescription{
            SDP:  msg.Data,
            Type: webrtc.SDPTypeOffer,
        }))

        answer, err := videoReceivers[msg.CurrentID].CreateAnswer(nil)
        showError(err)

        showError(videoReceivers[msg.CurrentID].SetLocalDescription(answer))

        reply := JSONMessage{
            Command: "send_sdp",
            CurrentID: msg.CurrentID,
            Data: answer.SDP,
        }

        replyMsg, err := json.Marshal(&reply)
        showError(err)
        showError(connection.WriteMessage(messageType, []byte(replyMsg)))

        //broadcast to every client connected
        stringReply := ""
        for k, _ := range videoReceivers {
            stringReply += k + ","
        }

        reply = JSONMessage{
            Command: "clients",
            CurrentID: msg.CurrentID,
            Data: stringReply,
        }

        broadcastMessage(reply, messageType)

    } else if (msg.Command == "send_sdp_in") {

        subSender, err := webrtcApi.NewPeerConnection(connectionConfig)
        showError(err)

        for {
            vl := videoTrackLocks[msg.CurrentID]
            vl.RLock()
            if videoTracks[msg.CurrentID] == nil {
                vl.RUnlock()
                time.Sleep(100 * time.Millisecond)
            } else {
                vl.RUnlock()
                break
            }
        }

        vl := videoTrackLocks[msg.CurrentID]
        vl.RLock()
        _, err = subSender.AddTrack(videoTracks[msg.CurrentID])
        vl.RUnlock()
        showError(err)

        al := audioTrackLocks[msg.CurrentID]
        al.RLock()
        _, err = subSender.AddTrack(audioTracks[msg.CurrentID])
        al.RUnlock()
        showError(err)

        showError(subSender.SetRemoteDescription(
        webrtc.SessionDescription{
            SDP:  msg.Data,
            Type: webrtc.SDPTypeOffer,
        }))

        answer, err := subSender.CreateAnswer(nil)
        showError(err)

        showError(subSender.SetLocalDescription(answer))

        reply := JSONMessage{
            Command: "send_sdp_in",
            CurrentID: msg.CurrentID,
            Data: answer.SDP,
        }

        replyMsg, err := json.Marshal(&reply)
        showError(err)
        showError(connection.WriteMessage(messageType, []byte(replyMsg)))
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