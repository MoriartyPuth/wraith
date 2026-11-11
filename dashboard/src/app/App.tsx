import { lazy } from "react";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { Shell } from "../layouts/Shell";
import { LiveProvider } from "../services/live";
import { Empty } from "../components/ui";

// One chunk per page: the first paint loads only the shell and that page.
const Overview = lazy(() => import("../pages/Overview"));
const WirelessMonitor = lazy(() => import("../pages/WirelessMonitor"));
const AccessPoints = lazy(() => import("../pages/AccessPoints"));
const APDetail = lazy(() => import("../pages/APDetail"));
const Clients = lazy(() => import("../pages/Clients"));
const SecurityEvents = lazy(() => import("../pages/SecurityEvents"));
const Detections = lazy(() => import("../pages/Detections"));
const PMFInspector = lazy(() => import("../pages/PMFInspector"));
const ChannelAnalysis = lazy(() => import("../pages/ChannelAnalysis"));
const Sensors = lazy(() => import("../pages/Sensors"));
const EventHistory = lazy(() => import("../pages/EventHistory"));
const Settings = lazy(() => import("../pages/Settings"));

export function App() {
  return (
    <BrowserRouter>
      <LiveProvider>
        <Routes>
          <Route element={<Shell />}>
            <Route index element={<Overview />} />
            <Route path="monitor" element={<WirelessMonitor />} />
            <Route path="access-points" element={<AccessPoints />} />
            <Route path="access-points/:bssid" element={<APDetail />} />
            <Route path="clients" element={<Clients />} />
            <Route path="events" element={<SecurityEvents />} />
            <Route path="detections" element={<Detections />} />
            <Route path="pmf" element={<PMFInspector />} />
            <Route path="channels" element={<ChannelAnalysis />} />
            <Route path="sensors" element={<Sensors />} />
            <Route path="history" element={<EventHistory />} />
            <Route path="settings" element={<Settings />} />
            <Route path="*" element={<Empty>Page not found.</Empty>} />
          </Route>
        </Routes>
      </LiveProvider>
    </BrowserRouter>
  );
}
