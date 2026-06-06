// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package sending

type failoverCarrier struct {
	id string
	n  int
}

func buildFailoverCarriers(route *RouteEntry, failoverEnabled bool) []failoverCarrier {
	carriers := []failoverCarrier{{id: route.PrimaryCarrierID, n: 0}}
	if !failoverEnabled {
		return carriers
	}
	if route.Failover1CarrierID != nil && *route.Failover1CarrierID != "" {
		carriers = append(carriers, failoverCarrier{id: *route.Failover1CarrierID, n: 1})
	}
	if route.Failover2CarrierID != nil && *route.Failover2CarrierID != "" {
		carriers = append(carriers, failoverCarrier{id: *route.Failover2CarrierID, n: 2})
	}
	return carriers
}
