import { Route, Routes } from "react-router-dom";

// Pages
import { Overview } from "@/pages/overview";
import { Social } from "@/pages/social";
import { Coming } from "@/pages/Coming";

export default function AppRoutes() {
  return (
    <Routes>
      <Route path="/" element={<Overview />} />
      <Route path="/social" element={<Social />} />

      <Route path="*" element={<Coming />} />
    </Routes>
  );
}