// based on Barix's Discovery tool that starts to fail
// KZ 2025.12.03.

package main

import (
    "bufio"
    "fmt"
    "net"
    "os"
    "os/exec"
    "runtime"
    "strconv"
    "strings"
    "time"
)

const (
    udpPort  = 30718
    timeout  = 2 * time.Second
    interval = 5 * time.Second
)

var (
    barixPrefix      = []byte{0x00, 0x08, 0xE1}
    discoveryPayload = []byte{0x81, 0x88, 0x53, 0x81, 0x01}
    setIPPrefix      = []byte{0x81, 0x88, 0x53, 0x81, 0x02}
)

const (
    colorInfoHex    = "#0d6efd"
    colorSuccessHex = "#28a745"
    colorErrorHex   = "#dc3545"
    colorWarnHex    = "#ffc107"
)

type Device struct {
    MAC []byte
    IP  net.IP
}

func hexToRGB(hex string) (int64, int64, int64) {
    hex = strings.TrimPrefix(hex, "#")
    if len(hex) != 6 {
        return 255, 255, 255
    }
    r, _ := strconv.ParseInt(hex[0:2], 16, 64)
    g, _ := strconv.ParseInt(hex[2:4], 16, 64)
    b, _ := strconv.ParseInt(hex[4:6], 16, 64)
    return r, g, b
}

func colorize(s, hex string) string {
    r, g, b := hexToRGB(hex)
    return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m", r, g, b, s)
}

func clearScreen() {
    fmt.Print("\x1b[2J\x1b[H")
}

func macToString(mac []byte) string {
    if len(mac) != 6 {
        return ""
    }
    return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X",
        mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
}

func sendDiscovery() {
    // Broadcast discovery payload to 255.255.255.255:udpPort
    addr := &net.UDPAddr{IP: net.IPv4bcast, Port: udpPort}
    conn, err := net.DialUDP("udp4", nil, addr)
    if err != nil {
        return
    }
    defer conn.Close()
    _, _ = conn.Write(discoveryPayload)
}

func discoverOnce() [][]byte {
    sendDiscovery()

    addr := &net.UDPAddr{IP: net.IPv4zero, Port: udpPort}
    conn, err := net.ListenUDP("udp4", addr)
    if err != nil {
        return nil
    }
    defer conn.Close()

    _ = conn.SetReadDeadline(time.Now().Add(timeout))

    var replies [][]byte
    buf := make([]byte, 2048)

    for {
        n, _, err := conn.ReadFromUDP(buf)
        if err != nil {
            // includes timeout
            break
        }
        data := make([]byte, n)
        copy(data, buf[:n])
        replies = append(replies, data)
    }

    return replies
}

func sendSetIPViaDiscovery(mac []byte, newIP string) error {
    if len(mac) != 6 {
        return fmt.Errorf("MAC must be 6 bytes long")
    }

    ipParsed := net.ParseIP(strings.TrimSpace(newIP))
    if ipParsed == nil {
        return fmt.Errorf("invalid IP address")
    }
    ipParsed = ipParsed.To4()
    if ipParsed == nil {
        return fmt.Errorf("IP must be IPv4")
    }

    payload := make([]byte, 0, len(setIPPrefix)+6+4)
    payload = append(payload, setIPPrefix...)
    payload = append(payload, mac...)
    payload = append(payload, ipParsed...)

    // Bind to udpPort+1 like Java's UDPSend and send broadcast to udpPort
    localAddr := &net.UDPAddr{IP: net.IPv4zero, Port: udpPort + 1}
    conn, err := net.ListenUDP("udp4", localAddr)
    if err != nil {
        // Fallback: ephemeral port
        conn, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
        if err != nil {
            return err
        }
    }
    defer conn.Close()

    remoteAddr := &net.UDPAddr{IP: net.IPv4bcast, Port: udpPort}
    _, err = conn.WriteToUDP(payload, remoteAddr)
    return err
}

func openBrowser(url string) error {
    var cmd *exec.Cmd

    switch runtime.GOOS {
    case "windows":
        cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
    case "darwin":
        cmd = exec.Command("open", url)
    default:
        cmd = exec.Command("xdg-open", url)
    }

    return cmd.Start()
}

func runSSH(ip string, username string) {
    target := fmt.Sprintf("%s@%s", username, ip)

    var cmd *exec.Cmd

    if runtime.GOOS == "windows" {
        // On Windows, try ssh (OpenSSH), then PuTTY, then plink
        if _, err := exec.LookPath("ssh"); err == nil {
            cmd = exec.Command("ssh", target)
        } else if _, err := exec.LookPath("putty"); err == nil {
            // PuTTY GUI; user will type password in its window
            cmd = exec.Command("putty", target)
        } else if _, err := exec.LookPath("plink"); err == nil {
            // plink (PuTTY's CLI tool)
            cmd = exec.Command("plink", target)
        } else {
            fmt.Println(colorize(
                "No SSH client found on PATH (tried ssh, putty, plink). Please install one.",
                colorErrorHex,
            ))
            return
        }
    } else {
        // Linux
        _ = exec.Command("ssh-keygen", "-R", ip).Run()
        cmd = exec.Command("ssh", target)
    }

    // Attach to the user's terminal so SSH can interact normally
    cmd.Stdin = os.Stdin
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    _ = cmd.Run()
}


func main() {
    devices := make(map[string]*Device) // key: MAC string
    order := make([]string, 0)          // to preserve discovery order

    reader := bufio.NewReader(os.Stdin)

    for {
        clearScreen()
        fmt.Println(colorize("Discovering Barix devices (CTRL-C to stop)", colorInfoHex))
        header := fmt.Sprintf("%2s  %-15s  %-17s", "#", "Device IP", "MAC Address")
        fmt.Println(colorize(header, colorInfoHex))
        fmt.Println(colorize(strings.Repeat("-", len(header)), colorInfoHex))

        for _, data := range discoverOnce() {
            if len(data) < 15 {
                continue
            }

            mac := make([]byte, 6)
            copy(mac, data[5:11])

            if len(mac) < 3 ||
                mac[0] != barixPrefix[0] ||
                mac[1] != barixPrefix[1] ||
                mac[2] != barixPrefix[2] {
                continue
            }

            ipBytes := data[11:15]
            ip := net.IPv4(ipBytes[0], ipBytes[1], ipBytes[2], ipBytes[3])

            macStr := macToString(mac)
            if _, exists := devices[macStr]; !exists {
                devices[macStr] = &Device{MAC: mac, IP: ip}
                order = append(order, macStr)
            } else {
                devices[macStr].IP = ip
            }
        }

        if len(order) == 0 {
            fmt.Println(colorize(time.Now().Format("2006-01-02 15:04:05 ")+" No devices found yet.", colorWarnHex))
        } else {
            for idx, macStr := range order {
                d := devices[macStr]
                fmt.Printf("%s\n", colorize(
                    fmt.Sprintf("%2d) %-15s  %-17s", idx+1, d.IP.String(), macStr),
                    colorSuccessHex,
                ))
            }
        }

        if len(order) > 0 {
            fmt.Print(colorize("Select device number to manage (or press Enter to refresh): ", colorInfoHex))
            line, _ := reader.ReadString('\n')
            line = strings.TrimSpace(line)

            if line != "" {
                num, err := strconv.Atoi(line)
                if err == nil && num >= 1 && num <= len(order) {
                    macStr := order[num-1]
                    d := devices[macStr]

                    fmt.Printf("\n%s\n", colorize(
                        fmt.Sprintf("Selected device #%d: %-15s  %-17s", num, d.IP.String(), macStr),
                        colorInfoHex,
                    ))

                    fmt.Println(colorize("What would you like to do with this device?", colorInfoHex))
                    fmt.Println(colorize("  1) SSH into device", colorInfoHex))
                    fmt.Println(colorize("  2) Change IP address now (UDP SET)", colorInfoHex))
                    fmt.Println(colorize("  3) Open Web UI to configure", colorInfoHex))
                    fmt.Println(colorize("  4) Cancel and return to discovery", colorInfoHex))

                    fmt.Print(colorize("Enter choice [1-4]: ", colorInfoHex))
                    action, _ := reader.ReadString('\n')
                    action = strings.TrimSpace(action)
                    if action == "" {
                        action = "4"
                    }

                    switch action {
                    case "1":
                        fmt.Print(colorize("SSH username: ", colorInfoHex))
                        username, _ := reader.ReadString('\n')
                        username = strings.TrimSpace(username)
                        if username != "" {
                            runSSH(d.IP.String(), username)
                        }
                    case "2":
                        fmt.Println(colorize("Enter the NEW IP address for this device.", colorWarnHex))
                        fmt.Print(colorize(
                            fmt.Sprintf("New IP for %s (current %s): ", macStr, d.IP.String()),
                            colorInfoHex,
                        ))
                        newIP, _ := reader.ReadString('\n')
                        newIP = strings.TrimSpace(newIP)
                        if newIP != "" {
                            if err := sendSetIPViaDiscovery(d.MAC, newIP); err != nil {
                                fmt.Println(colorize("Failed to send SET IP command: "+err.Error(), colorErrorHex))
                            } else {
                                d.IP = net.ParseIP(newIP)
                                if d.IP != nil {
                                    d.IP = d.IP.To4()
                                }
                                fmt.Println(colorize(
                                    fmt.Sprintf("Sent SET IP command: %s -> %s", macStr, newIP),
                                    colorSuccessHex,
                                ))
                            }
                        }
                        fmt.Print(colorize("\nPress Enter to return to discovery...", colorInfoHex))
                        _, _ = reader.ReadString('\n')
                    case "3":
                        url := fmt.Sprintf("http://%s/", d.IP.String())
                        fmt.Println(colorize("Opening "+url+" in your default browser so you can configure the device.", colorInfoHex))
                        if err := openBrowser(url); err != nil {
                            fmt.Println(colorize(
                                "Could not open browser automatically. Please open "+url+" manually. ("+err.Error()+")",
                                colorWarnHex,
                            ))
                        }
                        fmt.Print(colorize("\nPress Enter to return to discovery...", colorInfoHex))
                        _, _ = reader.ReadString('\n')
                    default:
                        // Cancel and return to discovery
                    }
                }
            }
        }

        time.Sleep(interval)
    }
}
