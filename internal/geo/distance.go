package geo

import "math"

type NodeLocation struct {
	ID  string
	Lat float64
	Lng float64
}

var edgeNodes = []NodeLocation{
	{ID: "us-east-1", Lat: 39.0481, Lng: -77.4728},
	{ID: "eu-west-1", Lat: 53.3331, Lng: -6.2489},
	{ID: "ap-northeast-1", Lat: 35.6762, Lng: 139.6503},
}

func HaversineDistance(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadiusKm = 6371.0

	dLat := toRad(lat2 - lat1)
	dLng := toRad(lng2 - lng1)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*
			math.Sin(dLng/2)*math.Sin(dLng/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusKm * c
}

func toRad(deg float64) float64 {
	return deg * math.Pi / 180
}

func ClosestNode(clientLat, clientLng float64) string {
	closest := edgeNodes[0]
	minDist := HaversineDistance(clientLat, clientLng, closest.Lat, closest.Lng)

	for _, node := range edgeNodes[1:] {
		d := HaversineDistance(clientLat, clientLng, node.Lat, node.Lng)
		if d < minDist {
			minDist = d
			closest = node
		}
	}
	return closest.ID
}

func DistanceToNode(clientLat, clientLng float64, nodeID string) float64 {
	for _, node := range edgeNodes {
		if node.ID == nodeID {
			return HaversineDistance(clientLat, clientLng, node.Lat, node.Lng)
		}
	}
	return -1
}
