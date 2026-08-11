import { useEffect } from 'react';
import './css/index.css';
import "../bridge/desktop";
import { reportHomeProbe } from './hooks/homeProbe';
import { runBootSequence } from './store/bootStore';
import { installHomeWatchers } from './store/libraryStore';
import { installPipelineWatchers } from './store/pipelineStore';
import { BootScreen } from './components/BootScreen';
import { TopBar } from './components/TopBar';
import { ToolsRow } from './components/ToolsRow';
import { VideoWall } from './components/VideoWall';
import { WindowDropOverlay } from './components/WindowDropOverlay';
import { ToastList } from './components/ToastList';
import { LoadingOverlay } from './components/LoadingOverlay';
import { AskModal } from './components/modals/AskModal';
import { SpeakerModal } from './components/modals/SpeakerModal';
import { GlossaryManagerModal } from './components/modals/GlossaryManagerModal';
import { SettingsModal } from './components/modals/SettingsModal';
import { NewsModal } from './components/modals/NewsModal';
import { LoginModal } from './components/modals/LoginModal';
import { UpdateGateModal } from './components/modals/UpdateGateModal';

export function App() {

  useEffect(() => {
    // Mac 平台的标题栏优化
    if (navigator.platform.startsWith("Mac")) document.body.classList.add("darwin");

    // 主页健康探测，通知主进程渲染进程状态
    const onError = (event: ErrorEvent) => reportHomeProbe(event.error || event.message);
    const onRejection = (event: PromiseRejectionEvent) => reportHomeProbe(event.reason);
    window.addEventListener("error", onError);
    window.addEventListener("unhandledrejection", onRejection);
    return () => {
      window.removeEventListener("error", onError);
      window.removeEventListener("unhandledrejection", onRejection);
    };
  }, []);

  useEffect(() => installHomeWatchers(), []);
  useEffect(() => installPipelineWatchers(), []);
  useEffect(() => runBootSequence(), []);

  return (
    <>
      {/* 启动态 + 登录页 */}
      <UpdateGateModal />
      <BootScreen />
      <LoginModal />

      {/* 主界面 */}
      <TopBar />
      <ToolsRow />
      <VideoWall />
      <WindowDropOverlay />

      {/* 功能类弹窗 */}
      <SpeakerModal />
      <GlossaryManagerModal />
      <SettingsModal />

      {/* 提示类弹窗 */}
      <AskModal />
      <LoadingOverlay />
      <ToastList />
      <NewsModal />
    </>
  );
}
