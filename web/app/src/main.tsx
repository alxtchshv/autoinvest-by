import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import "@fontsource-variable/onest";
import "./styles/base.css";
import "./styles/components.css";
import "./styles/layout.css";
import "./styles/pages.css";
import { SessionProvider } from "./lib/session";
import { HideProvider, ToastProvider } from "./components/ui";
import { AppShell, PublicLayout, RequireAuth } from "./components/layout";
import Landing from "./pages/public/Landing";
import { Forgot, Login, Register, Reset, VerifyEmail } from "./pages/public/Auth";
import Legal from "./pages/public/Legal";
import NotFound from "./pages/public/NotFound";
import Home from "./pages/app/Home";
import Plans from "./pages/app/Plans";
import PlanNew from "./pages/app/PlanNew";
import PlanDetail from "./pages/app/PlanDetail";
import Market from "./pages/app/Market";
import AssetPage from "./pages/app/Asset";
import Calculator from "./pages/app/Calculator";
import History from "./pages/app/History";
import Receipt from "./pages/app/Receipt";
import Statement from "./pages/app/Statement";
import Onboarding from "./pages/app/Onboarding";
import Cards from "./pages/app/Cards";
import SandboxPay from "./pages/app/SandboxPay";
import Notifications from "./pages/app/Notifications";
import { Support, TicketPage } from "./pages/app/Support";
import Profile from "./pages/app/Profile";
import { AdminLayout, AdminOverview, AdminUsers, AdminUser, AdminKYC, AdminTickets, AdminTicket, AdminOrders, AdminSystem } from "./pages/admin/Admin";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <BrowserRouter>
      <SessionProvider>
        <HideProvider>
          <ToastProvider>
            <Routes>
              <Route element={<PublicLayout />}>
                <Route index element={<Landing />} />
                <Route path="login" element={<Login />} />
                <Route path="register" element={<Register />} />
                <Route path="forgot-password" element={<Forgot />} />
                <Route path="reset-password" element={<Reset />} />
                <Route path="verify-email" element={<VerifyEmail />} />
                <Route path="legal" element={<Navigate to="/legal/offer" replace />} />
                <Route path="legal/:doc" element={<Legal />} />
                <Route path="calculator" element={<div className="wrapx" style={{ paddingTop: 24, paddingBottom: 64 }}><Calculator publicPage /></div>} />
                <Route path="*" element={<NotFound />} />
              </Route>
              <Route path="pay/sandbox/:ref" element={<RequireAuth><SandboxPay /></RequireAuth>} />
              <Route path="app" element={<RequireAuth><AppShell /></RequireAuth>}>
                <Route index element={<Home />} />
                <Route path="plans" element={<Plans />} />
                <Route path="plans/new" element={<PlanNew />} />
                <Route path="plans/:id" element={<PlanDetail />} />
                <Route path="market" element={<Market />} />
                <Route path="market/:symbol" element={<AssetPage />} />
                <Route path="calculator" element={<Calculator />} />
                <Route path="history" element={<History />} />
                <Route path="history/statement" element={<Statement />} />
                <Route path="history/:id" element={<Receipt />} />
                <Route path="onboarding" element={<Onboarding />} />
                <Route path="cards" element={<Cards />} />
                <Route path="notifications" element={<Notifications />} />
                <Route path="support" element={<Support />} />
                <Route path="support/:id" element={<TicketPage />} />
                <Route path="profile" element={<Navigate to="/app/profile/personal" replace />} />
                <Route path="profile/:tab" element={<Profile />} />
                <Route path="*" element={<NotFound inApp />} />
              </Route>
              <Route path="admin" element={<RequireAuth staff><AdminLayout /></RequireAuth>}>
                <Route index element={<AdminOverview />} />
                <Route path="users" element={<AdminUsers />} />
                <Route path="users/:id" element={<AdminUser />} />
                <Route path="kyc" element={<AdminKYC />} />
                <Route path="tickets" element={<AdminTickets />} />
                <Route path="tickets/:id" element={<AdminTicket />} />
                <Route path="orders" element={<AdminOrders />} />
                <Route path="system" element={<AdminSystem />} />
              </Route>
            </Routes>
          </ToastProvider>
        </HideProvider>
      </SessionProvider>
    </BrowserRouter>
  </StrictMode>,
);
