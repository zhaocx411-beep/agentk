package timeline

// CleanupEmptyAutoTracks removes only empty, unlocked, automatically-created
// video/audio tracks. The first video track is the project's permanent V1.
// Legacy tracks (createdBy omitted) and user-created tracks are preserved.
func CleanupEmptyAutoTracks(doc Doc) (Doc, []string) {
	next := doc.Clone()
	baseVideoTrackID := ""
	for _, track := range next.Tracks {
		if track.Kind == TrackVideo {
			baseVideoTrackID = track.TrackID
			break
		}
	}
	occupied := make(map[string]bool, len(next.Items))
	for _, item := range next.Items {
		occupied[item.TrackID] = true
	}
	removed := make([]string, 0)
	kept := make([]Track, 0, len(next.Tracks))
	for _, track := range next.Tracks {
		autoMediaTrack := (track.Kind == TrackVideo || track.Kind == TrackAudio) &&
			track.CreatedBy == TrackCreatedAuto
		if autoMediaTrack && !track.Locked && track.TrackID != baseVideoTrackID &&
			!occupied[track.TrackID] {
			removed = append(removed, track.TrackID)
			continue
		}
		kept = append(kept, track)
	}
	next.Tracks = kept
	return next, removed
}
