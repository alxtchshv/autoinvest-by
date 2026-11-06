import { lazy, StrictMode, Suspense } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import "@fontsource-variable/onest";
import "./styles/base.css";
import "./styles/components.css";
import "./styles/layout.css";
import "./styles/pages.css";
import { SessionProvider } from "./lib/session";
import { HideProvider, PageLoader, ToastProvider } from "./components/ui";
import { AppShell, PublicLayout, RequireAuth } from "./components/layout";
import Landing from "./pages/public/Landing";
import { Forgot, Login, Register, Reset, VerifyEmail } from "./pages/public/Auth";
import NotFound from "./pages/public/NotFound";

// Кабинет, админка и тяжёлые страницы (графики) грузятся отдельными частями: первый экран открывается быстрее.
const Legal = lazy(() => import("./pages/public/Legal"));
const Home = lazy(() => import("./pages/app/Home"));
const Plans = lazy(() => import("./pages/app/Plans"));
const PlanNew = lazy(() => import("./pages/app/PlanNew"));
const PlanDetail = lazy(() => import("./pages/app/PlanDetail"));
const Market = lazy(() => import("./pages/app/Market"));
const AssetPage = lazy(() => import("./pages/app/Asset"));
const Calculator = lazy(() => import("./pages/app/Calculator"));
const History = lazy(() => import("./pages/app/History"));
const Receipt = lazy(() => import("./pages/app/Receipt"));
const Statement = lazy(() => import("./pages/app/Statement"));
const Onboarding = lazy(() => import("./pages/app/Onboarding"));
const Cards = lazy(() => import("./pages/app/Cards"));
const SandboxPay = lazy(() => import("./pages/app/SandboxPay"));
const MsiSandbox = lazy(() => import("./pages/app/Verify"));
const OAuthSandbox = lazy(() => import("./pages/public/OAuthSandbox"));
const TelegramSandbox = lazy(() => import("./pages/app/TelegramSandbox"));
const Notifications = lazy(() => import("./pages/app/Notifications"));
const Profile = lazy(() => import("./pages/app/Profile"));
const Support = lazy(() => import("./pages/app/Support").then((m) => ({ default: m.Support })));
const TicketPage = lazy(() => import("./pages/app/Support").then((m) => ({ default: m.TicketPage })));
const AdminLayout = lazy(() => import("./pages/admin/Admin").then((m) => ({ default: m.AdminLayout })));
const AdminOverview = lazy(() => import("./pages/admin/Admin").then((m) => ({ default: m.AdminOverview })));
const AdminUsers = lazy(() => import("./pages/admin/Admin").then((m) => ({ default: m.AdminUsers })));
const AdminUser = lazy(() => import("./pages/admin/Admin").then((m) => ({ default: m.AdminUser })));
const AdminKYC = lazy(() => import("./pages/admin/Admin").then((m) => ({ default: m.AdminKYC })));
const AdminTickets = lazy(() => import("./pages/admin/Admin").then((m) => ({ default: m.AdminTickets })));
const AdminTicket = lazy(() => import("./pages/admin/Admin").then((m) => ({ default: m.AdminTicket })));
const AdminOrders = lazy(() => import("./pages/admin/Admin").then((m) => ({ default: m.AdminOrders })));
const AdminSystem = lazy(() => import("./pages/admin/Admin").then((m) => ({ default: m.AdminSystem })));

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <BrowserRouter>
      <SessionProvider>
        <HideProvider>
          <ToastProvider>
            <Suspense fallback={<PageLoader />}>
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
              <Route path="verify/msi-sandbox" element={<RequireAuth><MsiSandbox /></RequireAuth>} />
              <Route path="oauth-sandbox" element={<OAuthSandbox />} />
              <Route path="telegram-sandbox" element={<RequireAuth><TelegramSandbox /></RequireAuth>} />
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
            </Suspense>
          </ToastProvider>
        </HideProvider>
      </SessionProvider>
    </BrowserRouter>
  </StrictMode>,
);
