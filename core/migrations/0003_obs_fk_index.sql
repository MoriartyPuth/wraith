-- observations.event_row drives the ON DELETE CASCADE when events are pruned.
-- Without an index, each deleted event scans the whole observations table,
-- making retention pruning O(n^2) (measured: 1m13s to prune 20k events).
CREATE INDEX idx_obs_event_row ON observations(event_row);
