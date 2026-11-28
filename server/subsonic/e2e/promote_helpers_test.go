package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/core"
	"github.com/navidrome/navidrome/core/auth"
	"github.com/navidrome/navidrome/core/editjob"
	"github.com/navidrome/navidrome/core/organize"
	"github.com/navidrome/navidrome/core/rip"
	"github.com/navidrome/navidrome/core/tagedit"
	"github.com/navidrome/navidrome/model"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// startPromote wires Promote (#171) into ripper as startup does: the Library lib writable (editing
// on, the Inbox recorded, if there is one yet), and an edit queue with Organize's and Promote's job kinds, running on the real database
// until the spec ends, with any more job kinds handle registers. It returns the queue and the
// editor its moves are made with.
func startPromote(ripper *rip.Ripper, s tagedit.Scanner, lib model.Library, handle ...func(*editjob.Queue, *tagedit.Editor)) (*editjob.Queue, *tagedit.Editor) {
	GinkgoHelper()
	editing := core.LibraryEditing{Enabled: true, Libraries: []core.LibraryWritable{{ID: lib.ID, Name: lib.Name, Writable: true}}}
	if inbox, err := core.FindInboxLibrary(ctx, ds); err == nil && inbox != nil {
		editing.InboxID = inbox.ID
	}
	core.SetLibraryEditing(editing)
	DeferCleanup(core.SetLibraryEditing, core.LibraryEditing{})
	editor := tagedit.New(ds, s)
	queue := editjob.New(ds, nil, editjob.Kinds(ds, editor))
	kinds := organize.JobKinds(ds, editor)
	for kind, k := range kinds {
		queue.Handle(kind, k)
	}
	queue.Handle(model.EditPromote, ripper.PromoteJobKind(kinds[model.EditPromote]))
	for _, h := range handle {
		h(queue, editor)
	}
	ripper.SetEditQueue(queue)
	qctx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { queue.Run(qctx); close(done) }()
	DeferCleanup(func() { stop(); <-done })
	return queue, editor
}

// adminCall sends a request to the handler as the admin user.
func adminCall(h http.Handler, admin model.User, method, url string, body any) *httptest.ResponseRecorder {
	GinkgoHelper()
	var b []byte
	if body != nil {
		var err error
		b, err = json.Marshal(body)
		Expect(err).ToNot(HaveOccurred())
	}
	req := httptest.NewRequest(method, url, bytes.NewBuffer(b))
	token, err := auth.CreateToken(&admin)
	Expect(err).ToNot(HaveOccurred())
	req.Header.Set(consts.UIAuthorizationHeader, "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// promoteJob plans the Promote of the Rip jobID through the Rip API (api), then runs the plan it
// was shown, and returns the plan and the Promote's edit job, once it is done.
func promoteJob(api http.Handler, admin model.User, jobID string) (organize.PlanPage, model.EditJob) {
	GinkgoHelper()
	w := adminCall(api, admin, http.MethodPost, "/"+jobID+"/promote/plan", nil)
	Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
	var plan organize.PlanPage
	Expect(json.Unmarshal(w.Body.Bytes(), &plan)).To(Succeed())
	w = adminCall(api, admin, http.MethodPost, "/"+jobID+"/promote", map[string]string{"token": plan.Token})
	Expect(w.Code).To(Equal(http.StatusOK), w.Body.String())
	var job model.EditJob
	Expect(json.Unmarshal(w.Body.Bytes(), &job)).To(Succeed())
	Expect(job.Kind).To(Equal(model.EditPromote))
	Expect(job.State).To(Equal(model.EditJobDone))
	return plan, job
}
