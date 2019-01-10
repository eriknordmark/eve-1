// Copyright (c) 2017 Zededa, Inc.
// All rights reserved.

package types

import (
	"encoding/json"
	"errors"
	"github.com/eriknordmark/ipinfo"
	"github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"net"
	"time"
)

// Indexed by UUID
// If IsZedmanager is set we do not create boN but instead configure the EID
// locally. This will go away once ZedManager runs in a domU like any
// application.
type AppNetworkConfig struct {
	UUIDandVersion      UUIDandVersion
	DisplayName         string
	Activate            bool
	IsZedmanager        bool
	LegacyDataPlane     bool
	OverlayNetworkList  []OverlayNetworkConfig
	UnderlayNetworkList []UnderlayNetworkConfig
}

func (config AppNetworkConfig) Key() string {
	return config.UUIDandVersion.UUID.String()
}

func (config AppNetworkConfig) VerifyFilename(fileName string) bool {
	expect := config.Key() + ".json"
	ret := expect == fileName
	if !ret {
		log.Errorf("Mismatch between filename and contained uuid: %s vs. %s\n",
			fileName, expect)
	}
	return ret
}

func (status AppNetworkStatus) CheckPendingAdd() bool {
	return status.PendingAdd
}

func (status AppNetworkStatus) CheckPendingModify() bool {
	return status.PendingModify
}

func (status AppNetworkStatus) CheckPendingDelete() bool {
	return status.PendingDelete
}

func (status AppNetworkStatus) Pending() bool {
	return status.PendingAdd || status.PendingModify || status.PendingDelete
}

// Indexed by UUID
type AppNetworkStatus struct {
	UUIDandVersion UUIDandVersion
	AppNum         int
	Activated      bool
	PendingAdd     bool
	PendingModify  bool
	PendingDelete  bool
	DisplayName    string
	// Copy from the AppNetworkConfig; used to delete when config is gone.
	IsZedmanager        bool
	LegacyDataPlane     bool
	OverlayNetworkList  []OverlayNetworkStatus
	UnderlayNetworkList []UnderlayNetworkStatus
	MissingNetwork      bool // If any Missing flag is set in the networks
	// Any errros from provisioning the network
	Error     string
	ErrorTime time.Time
}

func (status AppNetworkStatus) Key() string {
	return status.UUIDandVersion.UUID.String()
}

func (status AppNetworkStatus) VerifyFilename(fileName string) bool {
	expect := status.Key() + ".json"
	ret := expect == fileName
	if !ret {
		log.Errorf("Mismatch between filename and contained uuid: %s vs. %s\n",
			fileName, expect)
	}
	return ret
}

// Global network config. For backwards compatibility with build artifacts
// XXX move to using DevicePortConfig in build?
// XXX remove since it uses old "Uplink" terms. Need to fix build etc
type DeviceNetworkConfig struct {
	Uplink      []string // ifname; all uplinks
	FreeUplinks []string // subset used for image downloads
}

// Array in timestamp aka priority order; first one is the most desired
// config to use
type DevicePortConfigList struct {
	PortConfigList []DevicePortConfig
}

// A complete set of configuration for all the ports used by zedrouter on the
// device
type DevicePortConfig struct {
	Version       DevicePortConfigVersion
	Key           string
	TimePriority  time.Time // All zero's is fallback lowest priority

	// Times when last ping test Failed/Succeeded.
	// All zeros means never tested.
	LastFailed    time.Time
	LastSucceeded time.Time

	Ports         []NetworkPortConfig
}

type DevicePortConfigVersion uint32

// When new fields and/or new semantics are added to DevicePortConfig a new
// version value is added here.
const (
	DPCInitial DevicePortConfigVersion = iota
	DPCIsMgmt                          // Require IsMgmt to be set for management ports
)

type NetworkProxyType uint8

// Values if these definitions should match the values
// given to the types in zapi.ProxyProto
const (
	NPT_HTTP NetworkProxyType = iota
	NPT_HTTPS
	NPT_SOCKS
	NPT_FTP
	NPT_NOPROXY
	NPT_LAST = 255
)

type ProxyEntry struct {
	Type   NetworkProxyType
	Server string
	Port   uint32
}

type ProxyConfig struct {
	Proxies    []ProxyEntry
	Exceptions string
	Pacfile    string
	// If Enable is set we use WPAD. If the URL is not set we try
	// the various DNS suffixes until we can download a wpad.dat file
	NetworkProxyEnable bool   // Enable WPAD
	NetworkProxyURL    string // Complete URL i.e., with /wpad.dat
	WpadURL            string // The URL determined from DNS
}

type DhcpConfig struct {
	Dhcp       DhcpType // If DT_STATIC use below
	AddrSubnet string   // In CIDR e.g., 192.168.1.44/24
	Gateway    net.IP
	DomainName string
	NtpServer  net.IP
	DnsServers []net.IP // If not set we use Gateway as DNS server
}

type NetworkPortConfig struct {
	IfName string
	Name   string // New logical name set by controller/model
	IsMgmt bool   // Used to talk to controller
	Free   bool   // Higher priority to talk to controller since no cost
	DhcpConfig
	ProxyConfig
}

type NetworkPortStatus struct {
	IfName string
	Name   string // New logical name set by controller/model
	IsMgmt bool   // Used to talk to controller
	Free   bool
	NetworkObjectConfig
	AddrInfoList []AddrInfo
	ProxyConfig
	Error     string
	ErrorTime time.Time
}

type AddrInfo struct {
	Addr             net.IP
	Geo              ipinfo.IPInfo
	LastGeoTimestamp time.Time
}

// Published to microservices which needs to know about ports and IP addresses
type DeviceNetworkStatus struct {
	Version DevicePortConfigVersion // From DevicePortConfig
	Ports   []NetworkPortStatus
}

func rotate(arr []string, amount int) []string {
	if len(arr) == 0 {
		return []string{}
	}
	amount = amount % len(arr)
	return append(append([]string{}, arr[amount:]...), arr[:amount]...)
}

// Return all management ports
func GetMgmtPortsAny(globalStatus DeviceNetworkStatus, rotation int) []string {
	return getMgmtPortsImpl(globalStatus, rotation, false, false)
}

// Return all free management ports
func GetMgmtPortsFree(globalStatus DeviceNetworkStatus, rotation int) []string {
	return getMgmtPortsImpl(globalStatus, rotation, true, false)
}

// Return all non-free management ports
func GetMgmtPortsNonFree(globalStatus DeviceNetworkStatus, rotation int) []string {
	return getMgmtPortsImpl(globalStatus, rotation, false, true)
}

// Returns the IfNames.
func getMgmtPortsImpl(globalStatus DeviceNetworkStatus, rotation int,
	freeOnly bool, nonfreeOnly bool) []string {

	var ports []string
	for _, us := range globalStatus.Ports {
		if freeOnly && !us.Free {
			continue
		}
		if nonfreeOnly && us.Free {
			continue
		}
		if globalStatus.Version >= DPCIsMgmt &&
			!us.IsMgmt {
			continue
		}
		ports = append(ports, us.IfName)
	}
	return rotate(ports, rotation)
}

// Return number of local IP addresses for all the management ports
// excluding link-local addresses
func CountLocalAddrAnyNoLinkLocal(globalStatus DeviceNetworkStatus) int {

	// Count the number of addresses which apply
	addrs, _ := getInterfaceAddr(globalStatus, false, "", false)
	return len(addrs)
}

// Return number of local IP addresses for all the management ports
// excluding link-local addresses
func CountLocalAddrAnyNoLinkLocalIf(globalStatus DeviceNetworkStatus,
	port string) int {

	// Count the number of addresses which apply
	addrs, _ := getInterfaceAddr(globalStatus, false, port, false)
	return len(addrs)
}

// Return a list of free management ports that have non link local IP addresses
// Used by LISP.
func GetMgmtPortsFreeNoLinkLocal(globalStatus DeviceNetworkStatus) []NetworkPortStatus {
	// Return MgmtPort list with valid non link local addresses
	links, _ := getInterfaceAndAddr(globalStatus, true, "", false)
	return links
}

// Return number of local IP addresses for all the free management ports
// excluding link-local addresses
func CountLocalAddrFreeNoLinkLocal(globalStatus DeviceNetworkStatus) int {

	// Count the number of addresses which apply
	addrs, _ := getInterfaceAddr(globalStatus, true, "", false)
	return len(addrs)
}

// Pick one address from all of the management ports, unless if port is set
// in which we pick from that port. Includes link-local addresses.
// We put addresses from the free management ports first in the list i.e.,
// returned for the lower 'pickNum'
func GetLocalAddrAny(globalStatus DeviceNetworkStatus, pickNum int,
	port string) (net.IP, error) {

	freeOnly := false
	includeLinkLocal := true
	return getLocalAddrImpl(globalStatus, pickNum, port, freeOnly,
		includeLinkLocal)
}

// Pick one address from all of the management ports, unless if port is set
// in which we pick from that port. Excludes link-local addresses.
// We put addresses from the free management ports first in the list i.e.,
// returned for the lower 'pickNum'
func GetLocalAddrAnyNoLinkLocal(globalStatus DeviceNetworkStatus, pickNum int,
	port string) (net.IP, error) {

	freeOnly := false
	includeLinkLocal := false
	return getLocalAddrImpl(globalStatus, pickNum, port, freeOnly,
		includeLinkLocal)
}

// Pick one address from the free management ports, unless if port is set
// in which we pick from that port. Excludes link-local addresses.
// We put addresses from the free management ports first in the list i.e.,
// returned for the lower 'pickNum'
func GetLocalAddrFreeNoLinkLocal(globalStatus DeviceNetworkStatus, pickNum int,
	port string) (net.IP, error) {

	freeOnly := true
	includeLinkLocal := false
	return getLocalAddrImpl(globalStatus, pickNum, port, freeOnly,
		includeLinkLocal)
}

func getLocalAddrImpl(globalStatus DeviceNetworkStatus, pickNum int,
	port string, freeOnly bool, includeLinkLocal bool) (net.IP, error) {

	// Count the number of addresses which apply
	addrs, err := getInterfaceAddr(globalStatus, freeOnly, port,
		includeLinkLocal)
	if err != nil {
		return net.IP{}, err
	}
	numAddrs := len(addrs)
	pickNum = pickNum % numAddrs
	return addrs[pickNum], nil
}

func getInterfaceAndAddr(globalStatus DeviceNetworkStatus, free bool, port string,
	includeLinkLocal bool) ([]NetworkPortStatus, error) {

	var links []NetworkPortStatus
	var ifname string
	if port != "" {
		ifname = AdapterToIfName(&globalStatus, port)
	} else {
		ifname = port
	}
	for _, us := range globalStatus.Ports {
		if globalStatus.Version >= DPCIsMgmt &&
			!us.IsMgmt {
			continue
		}
		if free && !us.Free {
			continue
		}
		// If ifname is set it should match
		if us.IfName != ifname && ifname != "" {
			continue
		}

		if includeLinkLocal {
			link := NetworkPortStatus{
				IfName: us.IfName,
				//Addrs: us.Addrs,
				AddrInfoList: us.AddrInfoList,
				Name:         us.Name,
			}
			links = append(links, link)
		} else {
			var addrs []AddrInfo
			var link NetworkPortStatus
			link.IfName = us.IfName
			link.Name = us.Name
			for _, a := range us.AddrInfoList {
				if !a.Addr.IsLinkLocalUnicast() {
					addrs = append(addrs, a)
				}
			}
			if len(addrs) > 0 {
				link.AddrInfoList = addrs
				links = append(links, link)
			}
		}
	}
	if len(links) != 0 {
		return links, nil
	} else {
		return []NetworkPortStatus{}, errors.New("No good MgmtPorts")
	}
}

// Check if an interface/adapter name is a port owned by zedrouter
func IsPort(globalStatus DeviceNetworkStatus, port string) bool {
	for _, us := range globalStatus.Ports {
		if us.Name != port && us.IfName != port {
			continue
		}
		return true
	}
	return false
}

// Check if an interface/adapter name is a management port
func IsMgmtPort(globalStatus DeviceNetworkStatus, port string) bool {
	for _, us := range globalStatus.Ports {
		if us.Name != port && us.IfName != port {
			continue
		}
		if globalStatus.Version >= DPCIsMgmt &&
			!us.IsMgmt {
			continue
		}
		return true
	}
	return false
}

// Check if an interface/adapter name is a free management port
func IsFreeMgmtPort(globalStatus DeviceNetworkStatus, port string) bool {
	for _, us := range globalStatus.Ports {
		if us.Name != port && us.IfName != port {
			continue
		}
		if globalStatus.Version >= DPCIsMgmt &&
			!us.IsMgmt {
			continue
		}
		return us.Free
	}
	return false
}

func GetMgmtPort(globalStatus DeviceNetworkStatus, port string) *NetworkPortStatus {
	for _, us := range globalStatus.Ports {
		if us.Name != port && us.IfName != port {
			continue
		}
		if globalStatus.Version >= DPCIsMgmt &&
			!us.IsMgmt {
			continue
		}
		return &us
	}
	return nil
}

// Given an address tell me its IfName
func GetMgmtPortFromAddr(globalStatus DeviceNetworkStatus, addr net.IP) string {
	for _, us := range globalStatus.Ports {
		if globalStatus.Version >= DPCIsMgmt &&
			!us.IsMgmt {
			continue
		}
		for _, i := range us.AddrInfoList {
			if i.Addr.Equal(addr) {
				return us.IfName
			}
		}
	}
	return ""
}

// Returns addresses based on free, ifname, and whether or not we want
// IPv6 link-locals. Only applies to management ports.
// If free is not set, the addresses from the free management ports are first.
func getInterfaceAddr(globalStatus DeviceNetworkStatus, free bool,
	port string, includeLinkLocal bool) ([]net.IP, error) {

	var freeAddrs []net.IP
	var nonfreeAddrs []net.IP
	var ifname string
	if port != "" {
		ifname = AdapterToIfName(&globalStatus, port)
	} else {
		ifname = port
	}
	for _, us := range globalStatus.Ports {
		if free && !us.Free {
			continue
		}
		if globalStatus.Version >= DPCIsMgmt &&
			!us.IsMgmt {
			continue
		}
		// If ifname is set it should match
		if us.IfName != ifname && ifname != "" {
			continue
		}
		var addrs []net.IP
		for _, i := range us.AddrInfoList {
			if includeLinkLocal || !i.Addr.IsLinkLocalUnicast() {
				addrs = append(addrs, i.Addr)
			}
		}
		if free {
			freeAddrs = append(freeAddrs, addrs...)
		} else {
			nonfreeAddrs = append(nonfreeAddrs, addrs...)
		}
	}
	addrs := append(freeAddrs, nonfreeAddrs...)
	if len(addrs) != 0 {
		return addrs, nil
	} else {
		return []net.IP{}, errors.New("No good IP address")
	}
}

// Return list of port names we will report in info and metrics
// Always include dbo1x0 for now.
// XXX What about non-management ports? XXX how will caller tag?
// Latter will move to a system app when we disaggregate
func ReportPorts(deviceNetworkStatus DeviceNetworkStatus) []string {
	var names []string
	names = append(names, "dbo1x0")
	for _, port := range deviceNetworkStatus.Ports {
		names = append(names, port.Name)
	}
	return names
}

// lookup port Name to find IfName
// Can also match on IfName
// If not found, return the adapter string
func AdapterToIfName(deviceNetworkStatus *DeviceNetworkStatus,
	adapter string) string {

	for _, p := range deviceNetworkStatus.Ports {
		if p.Name == adapter {
			log.Infof("AdapterToIfName: found %s for %s\n",
				p.IfName, adapter)
			return p.IfName
		}
	}
	for _, p := range deviceNetworkStatus.Ports {
		if p.IfName == adapter {
			log.Infof("AdapterToIfName: matched %s\n", adapter)
			return adapter
		}
	}
	log.Infof("AdapterToIfName: no match for %s\n", adapter)
	return adapter
}

type MapServerType uint8

const (
	MST_INVALID MapServerType = iota
	MST_MAPSERVER
	MST_SUPPORT_SERVER
	MST_LAST = 255
)

type MapServer struct {
	ServiceType MapServerType
	NameOrIp    string
	Credential  string
}

type ServiceLispConfig struct {
	MapServers    []MapServer
	IID           uint32
	Allocate      bool
	ExportPrivate bool
	EidPrefix     net.IP
	EidPrefixLen  uint32

	Experimental bool
}

type OverlayNetworkConfig struct {
	Name          string // From proto message
	EID           net.IP // Always EIDv6
	LispSignature string
	ACLs          []ACE
	AppMacAddr    net.HardwareAddr // If set use it for vif
	AppIPAddr     net.IP           // EIDv4 or EIDv6
	Network       uuid.UUID

	// Optional additional information
	AdditionalInfoDevice *AdditionalInfoDevice

	// These field are only for isMgmt. XXX remove when isMgmt is removed
	MgmtIID             uint32
	MgmtDnsNameToIPList []DnsNameToIP // Used to populate DNS for the overlay
	MgmtMapServers      []MapServer
}

type OverlayNetworkStatus struct {
	OverlayNetworkConfig
	VifInfo
	BridgeMac    net.HardwareAddr
	BridgeIPAddr string // The address for DNS/DHCP service in zedrouter
	HostName     string
}

type DhcpType uint8

const (
	DT_NOOP        DhcpType = iota
	DT_STATIC               // Device static config
	DT_PASSTHROUGH          // App passthrough e.g., to a bridge
	DT_SERVER               // Local server for app network
	DT_CLIENT               // Device client on external port
)

type UnderlayNetworkConfig struct {
	Name       string           // From proto message
	AppMacAddr net.HardwareAddr // If set use it for vif
	AppIPAddr  net.IP           // If set use DHCP to assign to app
	Network    uuid.UUID
	ACLs       []ACE
}

type UnderlayNetworkStatus struct {
	UnderlayNetworkConfig
	VifInfo
	BridgeMac      net.HardwareAddr
	BridgeIPAddr   string // The address for DNS/DHCP service in zedrouter
	AssignedIPAddr string // Assigned to domU
	HostName       string
}

type NetworkType uint8

const (
	NT_IPV4      NetworkType = 4
	NT_IPV6                  = 6
	NT_CryptoEID             = 14 // Either IPv6 or IPv4; adapter Addr
	// determines whether IPv4 EIDs are in use.
	// XXX Do we need a NT_DUAL/NT_IPV46? Implies two subnets/dhcp ranges?
	// XXX how do we represent a bridge? NT_L2??
)

// Extracted from the protobuf NetworkConfig
// Referenced using the UUID in Overlay/UnderlayNetworkConfig
// Note that NetworkConfig can be referenced (by UUID) from NetworkService.
// If there is no such reference the NetworkConfig ends up being local to the
// host.
type NetworkObjectConfig struct {
	UUID            uuid.UUID
	Type            NetworkType
	Dhcp            DhcpType // If DT_STATIC or DT_SERVER use below
	Subnet          net.IPNet
	Gateway         net.IP
	DomainName      string
	NtpServer       net.IP
	DnsServers      []net.IP // If not set we use Gateway as DNS server
	DhcpRange       IpRange
	DnsNameToIPList []DnsNameToIP // Used for DNS and ACL ipset
	Proxy           *ProxyConfig
}

type IpRange struct {
	Start net.IP
	End   net.IP
}

func (config NetworkObjectConfig) Key() string {
	return config.UUID.String()
}

type NetworkObjectStatus struct {
	NetworkObjectConfig
	PendingAdd    bool
	PendingModify bool
	PendingDelete bool
	BridgeNum     int
	BridgeName    string // bn<N>
	BridgeIPAddr  string

	// Used to populate DNS and eid ipset
	DnsNameToIPList []DnsNameToIP

	// Collection of address assignments; from MAC address to IP address
	IPAssignments map[string]net.IP

	// Union of all ipsets fed to dnsmasq for the linux bridge
	BridgeIPSets []string

	// Set of vifs on this bridge
	VifNames []string

	Ipv4Eid bool // Track if this is a CryptoEid with IPv4 EIDs

	// Any errrors from provisioning the network
	Error     string
	ErrorTime time.Time
}

func (status NetworkObjectStatus) Key() string {
	return status.UUID.String()
}

type NetworkServiceType uint8

const (
	NST_FIRST NetworkServiceType = iota
	NST_STRONGSWAN
	NST_LISP
	NST_BRIDGE
	NST_NAT // Default?
	NST_LB  // What is this?
	// XXX Add a NST_L3/NST_ROUTER to describe IP forwarding?
	NST_LAST = 255
)

// Extracted from protobuf Service definition
type NetworkServiceConfig struct {
	UUID         uuid.UUID
	Internal     bool // Internally created - not from zedcloud
	DisplayName  string
	Type         NetworkServiceType
	Activate     bool
	AppLink      uuid.UUID
	Adapter      string // Ifname or group like "uplink", or empty
	OpaqueConfig string
	LispConfig   ServiceLispConfig
}

func (config NetworkServiceConfig) Key() string {
	return config.UUID.String()
}

type NetworkServiceStatus struct {
	UUID          uuid.UUID
	PendingAdd    bool
	PendingModify bool
	PendingDelete bool
	DisplayName   string
	Type          NetworkServiceType
	Activated     bool
	AppLink       uuid.UUID
	Adapter       string // Ifname or group like "uplink", or empty
	OpaqueStatus  string
	LispStatus    ServiceLispConfig
	IfNameList    []string  // Recorded at time of activate
	Subnet        net.IPNet // Recorded at time of activate

	MissingNetwork bool // If AppLink UUID not found
	// Any errrors from provisioning the service
	Error          string
	ErrorTime      time.Time
	VpnStatus      *ServiceVpnStatus
	LispInfoStatus *LispInfoStatus
	LispMetrics    *LispMetrics
}

func (status NetworkServiceStatus) Key() string {
	return status.UUID.String()
}

type NetworkServiceMetrics struct {
	UUID        uuid.UUID
	DisplayName string
	Type        NetworkServiceType
	VpnMetrics  *VpnMetrics
	LispMetrics *LispMetrics
}

func (metrics NetworkServiceMetrics) Key() string {
	return metrics.UUID.String()
}

// Network metrics for overlay and underlay
// Matches networkMetrics protobuf message
type NetworkMetrics struct {
	MetricList []NetworkMetric
}

type NetworkMetric struct {
	IfName              string
	TxBytes             uint64
	RxBytes             uint64
	TxDrops             uint64
	RxDrops             uint64
	TxPkts              uint64
	RxPkts              uint64
	TxErrors            uint64
	RxErrors            uint64
	TxAclDrops          uint64 // For implicit deny/drop at end
	RxAclDrops          uint64 // For implicit deny/drop at end
	TxAclRateLimitDrops uint64 // For all rate limited rules
	RxAclRateLimitDrops uint64 // For all rate limited rules
}

// XXX this works but ugly as ...
// Alternative seems to be a deep walk with type assertions in order
// to produce the map of map of map with the correct type.
func CastNetworkMetrics(in interface{}) NetworkMetrics {
	b, err := json.Marshal(in)
	if err != nil {
		log.Fatal(err, "json Marshal in CastNetworkMetrics")
	}
	var output NetworkMetrics
	if err := json.Unmarshal(b, &output); err != nil {
		log.Fatal(err, "json Unmarshal in CastNetworkMetrics")
	}
	return output
}

// Similar support as in draft-ietf-netmod-acl-model
type ACE struct {
	Matches []ACEMatch
	Actions []ACEAction
}

// The Type can be "ip" or "host" (aka domain name), "eidset", "protocol",
// "fport", or "lport" for now. The ip and host matches the remote IP/hostname.
// The host matching is suffix-matching thus zededa.net matches *.zededa.net.
// XXX Need "interface"... e.g. "uplink" or "eth1"? Implicit in network used?
// For now the matches are bidirectional.
// XXX Add directionality? Different ragte limits in different directions?
// Value is always a string.
// There is an implicit reject rule at the end.
// The "eidset" type is special for the overlay. Matches all the IPs which
// are part of the DnsNameToIPList.
type ACEMatch struct {
	Type  string
	Value string
}

type ACEAction struct {
	Drop bool // Otherwise accept

	Limit      bool   // Is limiter enabled?
	LimitRate  int    // Packets per unit
	LimitUnit  string // "s", "m", "h", for second, minute, hour
	LimitBurst int    // Packets

	PortMap    bool // Is port mapping part of action?
	TargetPort int  // Internal port
}

// Retrieved from geolocation service for device underlay connectivity
type AdditionalInfoDevice struct {
	UnderlayIP string
	Hostname   string `json:",omitempty"` // From reverse DNS
	City       string `json:",omitempty"`
	Region     string `json:",omitempty"`
	Country    string `json:",omitempty"`
	Loc        string `json:",omitempty"` // Lat and long as string
	Org        string `json:",omitempty"` // From AS number
}

// Tie the Application EID back to the device
type AdditionalInfoApp struct {
	DisplayName string
	DeviceEID   net.IP
	DeviceIID   uint32
	UnderlayIP  string
	Hostname    string `json:",omitempty"` // From reverse DNS
}

// Input Opaque Config
type StrongSwanServiceConfig struct {
	VpnRole          string
	PolicyBased      bool
	IsClient         bool
	VpnGatewayIpAddr string
	VpnSubnetBlock   string
	VpnLocalIpAddr   string
	VpnRemoteIpAddr  string
	PreSharedKey     string
	LocalSubnetBlock string
	ClientConfigList []VpnClientConfig
}

// structure for internal handling
type VpnServiceConfig struct {
	VpnRole          string
	PolicyBased      bool
	IsClient         bool
	PortConfig       NetLinkConfig
	AppLinkConfig    NetLinkConfig
	GatewayConfig    NetLinkConfig
	ClientConfigList []VpnClientConfig
}

type NetLinkConfig struct {
	Name        string
	IpAddr      string
	SubnetBlock string
}

type VpnClientConfig struct {
	IpAddr       string
	SubnetBlock  string
	PreSharedKey string
	TunnelConfig VpnTunnelConfig
}

type VpnTunnelConfig struct {
	Name         string
	Key          string
	Mtu          string
	Metric       string
	LocalIpAddr  string
	RemoteIpAddr string
}

type LispRlocState struct {
	Rloc      net.IP
	Reachable bool
}

type LispMapCacheEntry struct {
	EID   net.IP
	Rlocs []LispRlocState
}

type LispDatabaseMap struct {
	IID             uint64
	MapCacheEntries []LispMapCacheEntry
}

type LispDecapKey struct {
	Rloc     net.IP
	Port     uint64
	KeyCount uint64
}

type LispInfoStatus struct {
	ItrCryptoPort uint64
	EtrNatPort    uint64
	Interfaces    []string
	DatabaseMaps  []LispDatabaseMap
	DecapKeys     []LispDecapKey
}

type LispPktStat struct {
	Pkts  uint64
	Bytes uint64
}

type LispRlocStatistics struct {
	Rloc                   net.IP
	Stats                  LispPktStat
	SecondsSinceLastPacket uint64
}

type EidStatistics struct {
	IID       uint64
	Eid       net.IP
	RlocStats []LispRlocStatistics
}

type EidMap struct {
	IID  uint64
	Eids []net.IP
}

type LispMetrics struct {
	// Encap Statistics
	EidMaps            []EidMap
	EidStats           []EidStatistics
	ItrPacketSendError LispPktStat
	InvalidEidError    LispPktStat

	// Decap Statistics
	NoDecryptKey       LispPktStat
	OuterHeaderError   LispPktStat
	BadInnerVersion    LispPktStat
	GoodPackets        LispPktStat
	ICVError           LispPktStat
	LispHeaderError    LispPktStat
	CheckSumError      LispPktStat
	DecapReInjectError LispPktStat
	DecryptError       LispPktStat
}

type LispDataplaneConfig struct {
	// If true, we run legacy lispers.net data plane.
	Legacy bool
}

type VpnState uint8

const (
	VPN_INVALID VpnState = iota
	VPN_INITIAL
	VPN_CONNECTING
	VPN_ESTABLISHED
	VPN_INSTALLED
	VPN_REKEYED
	VPN_DELETED  VpnState = 10
	VPN_MAXSTATE VpnState = 255
)

type VpnLinkInfo struct {
	SubNet    string // connecting subnet
	SpiId     string // security parameter index
	Direction bool   // 0 - in, 1 - out
	PktStats  PktStats
}

type VpnLinkStatus struct {
	Id         string
	Name       string
	ReqId      string
	InstTime   uint64 // installation time
	ExpTime    uint64 // expiry time
	RekeyTime  uint64 // rekey time
	EspInfo    string
	State      VpnState
	LInfo      VpnLinkInfo
	RInfo      VpnLinkInfo
	MarkDelete bool
}

type VpnEndPoint struct {
	Id     string // ipsec id
	IpAddr string // end point ip address
	Port   uint32 // udp port
}

type VpnConnStatus struct {
	Id         string   // ipsec connection id
	Name       string   // connection name
	State      VpnState // vpn state
	Version    string   // ike version
	Ikes       string   // ike parameters
	EstTime    uint64   // established time
	ReauthTime uint64   // reauth time
	LInfo      VpnEndPoint
	RInfo      VpnEndPoint
	Links      []*VpnLinkStatus
	StartLine  uint32
	EndLine    uint32
	MarkDelete bool
}

type ServiceVpnStatus struct {
	Version            string    // strongswan package version
	UpTime             time.Time // service start time stamp
	IpAddrs            string    // listening ip addresses, can be multiple
	ActiveVpnConns     []*VpnConnStatus
	StaleVpnConns      []*VpnConnStatus
	ActiveTunCount     uint32
	ConnectingTunCount uint32
	PolicyBased        bool
}

type PktStats struct {
	Pkts  uint64
	Bytes uint64
}

type LinkPktStats struct {
	InPkts  PktStats
	OutPkts PktStats
}

type VpnLinkMetrics struct {
	SubNet string // connecting subnet
	SpiId  string // security parameter index
}

type VpnEndPointMetrics struct {
	IpAddr   string // end point ip address
	LinkInfo VpnLinkMetrics
	PktStats PktStats
}

type VpnConnMetrics struct {
	Id        string // ipsec connection id
	Name      string // connection name
	EstTime   uint64 // established time
	Type      NetworkServiceType
	LEndPoint VpnEndPointMetrics
	REndPoint VpnEndPointMetrics
}

type VpnMetrics struct {
	UpTime     time.Time // service start time stamp
	DataStat   LinkPktStats
	IkeStat    LinkPktStats
	NatTStat   LinkPktStats
	EspStat    LinkPktStats
	ErrStat    LinkPktStats
	PhyErrStat LinkPktStats
	VpnConns   []*VpnConnMetrics
}
