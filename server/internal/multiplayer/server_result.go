package multiplayer

import "errors"

const localRoomRequestRejected = "-1"

func (c *clientConn) writeRoomRequestRejected(method string, cause error) error {
	if method == "" {
		return errors.New("room request result method is empty")
	}
	message := "local room request rejected"
	if cause != nil && cause.Error() != "" {
		message = cause.Error()
	}
	c.server.logger.Info(
		"local multiplayer room request rejected",
		"method", method,
		"room_id", c.roomID,
		"member_type", c.memberType,
		"user_id", c.userID,
		"reason", message,
	)
	code := localRoomRequestRejected
	if method == "RoomEnterRequestResult" {
		if errors.Is(cause, ErrRoomArthurUnavailable) {
			code = "-3202" // CN TEAMBATTLE_ROOM_ARTHUR_TYPE_FILL.
		} else if errors.Is(cause, ErrRoomUnavailable) {
			code = "-3208" // CN TEAMBATTLE_ROOM_NOT_FOUND.
		}
	}
	// The original consumer uses RoomInfo only on success. A failed request
	// needs just res_code,res_str, without a fake room object.
	return c.writeFrame(method, joinCSV(code, message))
}
