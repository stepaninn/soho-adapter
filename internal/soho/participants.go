package soho

type participantRoles struct {
	profiles  map[string]uint8
	authUsers map[int64]uint8
}

func newParticipantRoles(bundle HomeworkBundle) participantRoles {
	index := participantRoles{profiles: map[string]uint8{}, authUsers: map[int64]uint8{}}
	add := func(participants []ChatParticipant, mask uint8) {
		for _, participant := range participants {
			user := participant.User
			if user == nil {
				continue
			}
			// ID участника ChatParticipant не является ID профиля UserProfileCommon
			if user.ID != "" {
				index.profiles[user.ID] |= mask
			}
			if user.AuthUserID != nil && *user.AuthUserID > 0 {
				index.authUsers[*user.AuthUserID] |= mask
			}
		}
	}
	add(bundle.Teachers, 1)
	if bundle.StudentHomework != nil || (bundle.ChatPeer != nil && bundle.ChatPeer.TypeName == "ChatPeerHomework") {
		add(bundle.Students, 2)
	}
	return index
}

func (index participantRoles) resolve(author *MessageAuthor) (string, string, bool) {
	if author == nil {
		return "unknown", "unresolved", false
	}
	profileMask := index.profiles[author.ID]
	var authMask uint8
	if author.AuthUserID != nil && *author.AuthUserID > 0 {
		authMask = index.authUsers[*author.AuthUserID]
	}
	mask := profileMask | authMask
	if mask == 3 {
		return "unknown", "conflicting-participant-identities", true
	}
	role := "unknown"
	if mask == 1 {
		role = "teacher"
	} else if mask == 2 {
		role = "student"
	} else {
		return role, "unresolved", false
	}
	if profileMask != 0 {
		return role, "chat-participant-profile-id", false
	}
	return role, "chat-participant-auth-user-id", false
}
