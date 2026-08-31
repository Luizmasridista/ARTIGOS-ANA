package app

import (
	"net"
	"net/http"
)

func handleInfo(w http.ResponseWriter, r *http.Request) {
	ip := detectarLANIP()
	url := "http://localhost:8734"
	if ip != "" {
		url = "http://" + ip + ":8734"
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"ip":  ip,
		"url": url,
	})
}

func detectarLANIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if ipv4 := ip.To4(); ipv4 != nil && !ipv4.IsPrivate() {
				continue
			}
			if ip.To4() != nil {
				return ip.String()
			}
		}
	}
	return ""
}
