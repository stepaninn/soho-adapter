package soho

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestParticipantRoleResolution(t *testing.T) {
	studentAuth, teacherAuth := int64(701), int64(702)
	bundle := HomeworkBundle{StudentHomework: &StudentHomework{}, Teachers: []ChatParticipant{
		{ID: "teacher-participant", User: &MessageAuthor{ID: "teacher-profile", AuthUserID: &teacherAuth}},
		{ID: "head-participant", User: &MessageAuthor{ID: "head-profile"}},
		{ID: "missing-profile"},
	}, Students: []ChatParticipant{{ID: "student-participant", User: &MessageAuthor{ID: "student-profile", AuthUserID: &studentAuth}}}}
	index := newParticipantRoles(bundle)
	for _, tt := range []struct {
		name         string
		author       *MessageAuthor
		role, source string
		conflict     bool
	}{
		{"teacher profile", &MessageAuthor{ID: "teacher-profile"}, "teacher", "chat-participant-profile-id", false},
		{"head teacher profile", &MessageAuthor{ID: "head-profile"}, "teacher", "chat-participant-profile-id", false},
		{"student profile with different numeric UID", &MessageAuthor{ID: "student-profile", UID: 999}, "student", "chat-participant-profile-id", false},
		{"another profile with same auth user", &MessageAuthor{ID: "student-other-profile", AuthUserID: &studentAuth}, "student", "chat-participant-auth-user-id", false},
		{"numeric UID is not auth user ID", &MessageAuthor{ID: "unknown", UID: 701}, "unknown", "unresolved", false},
		{"participant ID is not profile ID", &MessageAuthor{ID: "student-participant"}, "unknown", "unresolved", false},
		{"missing author", nil, "unknown", "unresolved", false},
		{"conflicting identifiers", &MessageAuthor{ID: "teacher-profile", AuthUserID: &studentAuth}, "unknown", "conflicting-participant-identities", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			role, source, conflict := index.resolve(tt.author)
			if role != tt.role || source != tt.source || conflict != tt.conflict {
				t.Fatalf("role=%s source=%s conflict=%v", role, source, conflict)
			}
		})
	}
	bundle.Teachers = append(bundle.Teachers, ChatParticipant{User: &MessageAuthor{ID: "student-profile"}})
	if role, _, conflict := newParticipantRoles(bundle).resolve(&MessageAuthor{ID: "student-profile"}); role != "unknown" || !conflict {
		t.Fatal("participant with two roles was classified")
	}
	bundle.Teachers = nil
	bundle.StudentHomework = nil
	bundle.ChatPeer = nil
	if role, _, _ := newParticipantRoles(bundle).resolve(&MessageAuthor{ID: "student-profile"}); role != "unknown" {
		t.Fatal("Partner treated as a student outside homework context")
	}
}

func TestParticipantAutomaticMaterials(t *testing.T) {
	c := inboxTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		request, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		for _, fragment := range []string{"teachers: participants(roles: [Teacher, HeadTeacher])", "students: participants(roles: [Partner])", "user { id authUserId }", "userCreated { id uid name authUserId }"} {
			if !strings.Contains(string(request), fragment) {
				t.Errorf("missing role query: %s", fragment)
			}
		}
		_, _ = io.WriteString(w, `{"data":{"chat":{
   "__typename":"Chat","id":"chat","uid":42,
   "studentHomework":{"assignedTeacher":null,"academicHomework":{"id":"assignment"}},
   "teachers":[{"id":"participant-teacher","user":{"id":"profile-teacher","authUserId":702}}],
   "students":[{"id":"participant-student","user":{"id":"profile-student","authUserId":701}}],
   "messagesNew":{"pageInfo":{"hasPreviousPage":false,"hasNextPage":false},"edges":[
    {"node":{"id":"teacher-message","chatId":42,"userCreated":{"id":"profile-teacher","uid":21,"name":"Одинаковое имя","authUserId":702},"text":"<p>Пояснение к заданию</p><a href=\"https://task.test/\">Задание</a><img src=\"https://files.test/task.png\">"}},
    {"node":{"id":"student-message","chatId":42,"userCreated":{"id":"profile-student","uid":22,"name":"Одинаковое имя","authUserId":701},"metaInfo":"{\"typ\":\"MultiFile\",\"payload\":{\"files\":[{\"url\":\"https://files.test/answer.pdf\",\"mimeType\":\"application/pdf\"}]}}"}},
    {"node":{"id":"student-status","chatId":42,"userCreated":{"id":"profile-alias","uid":701,"authUserId":701},"metaInfo":"{\"typ\":\"HomeworkStatus\",\"payload\":{\"attempt\":0}}"}}
   ]}
  }}}`)
	})
	bundle, err := c.GetHomeworkBundle(context.Background(), "chat", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Teachers) != 1 || len(bundle.Students) != 1 {
		t.Fatal("participants not decoded")
	}
	materials := NormalizeHomework(bundle)
	for i, role := range []string{"teacher", "student", "student"} {
		message := materials.Messages[i]
		if message.Role != role || !strings.HasPrefix(message.RoleSource, "chat-participant-") {
			t.Fatalf("message %d: %s %s", i, message.Role, message.RoleSource)
		}
		for _, attachment := range message.Attachments {
			if attachment.Role != role || attachment.Author.ID != message.Author.ID {
				t.Fatal("file ownership lost")
			}
		}
	}
	if materials.Messages[0].Links[0].Role != "teacher" || !strings.Contains(materials.Messages[0].PlainText, "Пояснение") {
		t.Fatal("link or explanation ownership lost")
	}
	overridden := NormalizeHomeworkWithRoles(bundle, map[string]string{"profile-student": "teacher"})
	if overridden.Messages[1].Role != "student" {
		t.Fatal("manual fallback replaced authoritative API role")
	}
}
