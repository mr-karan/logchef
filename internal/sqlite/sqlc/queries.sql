-- name: ListTeams :many
SELECT t.id, t.name, t.description, t.created_at, t.updated_at,
       COUNT(tm.user_id) as member_count
FROM teams t
LEFT JOIN team_members tm ON t.id = tm.team_id
GROUP BY t.id
ORDER BY t.created_at DESC;