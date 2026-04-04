package clusteruser

// ClusterUser is the sync-layer global user metadata.
// It is distinct from the local usermgr user.
type ClusterUser struct {
	Username    string
	Password    string
	Expire      int64  // absolute expiry Unix timestamp (seconds); 0 = never expires
	Role        string
	TargetGroup string
	Deleted     bool
	UpdatedAtUs int64  // version timestamp (microseconds)
	OriginNode  string // node that produced this version
	Hash        string // digest of all canonical fields
}

// UserDigest is the compact form carried in heartbeats for delta comparison.
type UserDigest struct {
	Username    string
	UpdatedAtUs int64
	OriginNode  string
	Deleted     bool
	Hash        string
}
