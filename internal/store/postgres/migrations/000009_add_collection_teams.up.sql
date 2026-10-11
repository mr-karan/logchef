-- Share a collection with whole teams. A team share grants every current
-- member of the team the collection Member role (read + curate items). It never
-- grants Owner/Editor rights and never grants source access: membership is
-- resolved at request time, so joining or leaving the team changes visibility
-- immediately. Team users are not copied into collection_members.
CREATE TABLE collection_teams (
    collection_id BIGINT NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    team_id       BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    added_by      BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (collection_id, team_id)
);
CREATE INDEX idx_collection_teams_team ON collection_teams(team_id);
