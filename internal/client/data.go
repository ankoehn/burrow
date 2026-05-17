package client

import (
	"net"

	"github.com/hashicorp/yamux"

	"github.com/ankoehn/burrow/internal/bridge"
	"github.com/ankoehn/burrow/internal/proto"
)

// handleNewConnection opens a data stream, announces it, dials the local
// target for the tunnel, and bridges the two until either side closes.
func (c *Client) handleNewConnection(sess *yamux.Session, nc proto.NewConnection, localAddr string) {
	st, err := sess.OpenStream()
	if err != nil {
		return
	}
	defer st.Close()
	if err := proto.WriteMessage(st, proto.MsgStreamOpen, proto.StreamHeader{
		StreamID: nc.StreamID, TunnelID: nc.TunnelID,
	}); err != nil {
		return
	}
	local, err := net.Dial("tcp", localAddr)
	if err != nil {
		c.log.Warn("local dial failed", "tunnel_id", nc.TunnelID, "local", localAddr, "err", err)
		return
	}
	defer local.Close()
	var dummyIn, dummyOut atomicCounter
	bridge.Pipe(local, st, &dummyIn.v, &dummyOut.v)
}
