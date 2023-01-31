package imds

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	tagTupleSeparator    = ";"
	tagKeyValueSeparator = ":"
)

type InstanceMetadata struct {
	Compute ComputeInfo
	Network NetworkInfo
}

func NormalizeInstanceMetadata(i *InstanceMetadata) {
	// sanitize some fields as Azure seems to flip between upper and lowercases on returning them
	i.Compute.Location = strings.ToLower(i.Compute.Location)
	i.Compute.ResourceGroup = strings.ToLower(i.Compute.ResourceGroup)
	i.Compute.SubscriptionID = strings.ToLower(i.Compute.SubscriptionID)
	i.Compute.VMName = strings.ToLower(i.Compute.VMName)
	i.Compute.VMScaleSetName = strings.ToLower(i.Compute.VMScaleSetName)
}

func InstanceMetadataFromJSON(b []byte) (res InstanceMetadata, err error) {
	if err = json.Unmarshal(b, &res); err != nil {
		return res, fmt.Errorf("failed to unmarshal instance metadata json: %w", err)
	}

	return res, nil
}

type ComputeInfo struct {
	Cloud                string         `json:"azEnvironment"`
	CustomData           string         `json:"customData"`
	Location             string         `json:"location"`
	Offer                string         `json:"offer"`
	OSType               string         `json:"osType"`
	PlacementGroupID     string         `json:"placementGroupId"`
	Plan                 Plan           `json:"plan"`
	PlatformFaultDomain  string         `json:"platformFaultDomain"`
	PlatformUpdateDomain string         `json:"platformUpdateDomain"`
	Provider             string         `json:"provider"`
	PublicKeys           []PublicKey    `json:"publicKeys"`
	Publisher            string         `json:"publisher"`
	ResourceGroup        string         `json:"resourceGroupName"`
	ResourceID           string         `json:"resourceId"`
	StorageProfile       StorageProfile `json:"storageProfile"`
	SKU                  string         `json:"sku"`
	SubscriptionID       string         `json:"subscriptionId"`
	TagsString           string         `json:"tags"`
	TagsList             []Tag          `json:"tagsList"`
	VMID                 string         `json:"vmID"`
	VMName               string         `json:"name"`
	VMScaleSetName       string         `json:"vmScaleSetName"`
	VMSize               string         `json:"vmSize"`
	Zone                 string         `json:"zone"`
}

// Tags returns the VM tags as a map[string]string. This method is preferable to retrieving the raw values from
// TagsString or TagsList because it safely handles the situation where older IMDS versions do not return the TagsList
// but do returns the Tags as a string.
func (i *ComputeInfo) Tags() map[string]string {
	// older IMDS API versions do not support tagsList but do send tags in the "tags" string. This string can be parsed
	if len(i.TagsList) == 0 && i.TagsString != "" {
		return i.tagsStringToMap()
	}

	return i.tagsListToMap()
}

func (i *ComputeInfo) tagsStringToMap() map[string]string {
	res := make(map[string]string)

	for _, tagPair := range strings.Split(i.TagsString, tagTupleSeparator) {
		name := tagPair[0:strings.Index(tagPair, tagKeyValueSeparator)]
		value := tagPair[strings.Index(tagPair, tagKeyValueSeparator)+1:]
		res[name] = value
	}

	return res
}

func (i *ComputeInfo) tagsListToMap() map[string]string {
	res := make(map[string]string)

	for _, tagPair := range i.TagsList {
		res[tagPair.Name] = tagPair.Value
	}

	return res
}

type Tag struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type PublicKey struct {
	KeyData string `json:"keyData"`
	Path    string `json:"path"`
}

type StorageProfile struct {
	ImageReference ImageReference
}

type ImageReference struct {
	ID        string `json:"id"`
	Offer     string `json:"offer"`
	Publisher string `json:"publisher"`
	SKU       string `json:"sku"`
	Version   string `json:"version"`
}

type Plan struct {
	Name      string `json:"name"`
	Product   string `json:"product"`
	Publisher string `json:"publisher"`
}

type NetworkInfo struct {
	Interfaces []NetworkInterface `json:"interface"`
}

type NetworkInterface struct {
	IPv4       IPv4   `json:"ipv4"`
	IPv6       IPv6   `json:"ipv6"`
	MACAddress string `json:"macAddress"`
}

type IPv4 struct {
	Addresses []IPv4Address `json:"ipAddress"`
	Subnet    []IPv4Subnet  `json:"subnet"`
}

type IPv4Address struct {
	Private string `json:"privateIpAddress"`
	Public  string `json:"publicIpAddress"`
}

type IPv4Subnet struct {
	Address string
	Prefix  string
}

func (ipv4 *IPv4Subnet) CIDR() string {
	return fmt.Sprintf("%s/%s", ipv4.Address, ipv4.Prefix)
}

type IPv6 struct {
	Addresses []IPv6Address `json:"ipAddress"`
}

type IPv6Address struct {
	Private string `json:"privateIpAddress"`
}
