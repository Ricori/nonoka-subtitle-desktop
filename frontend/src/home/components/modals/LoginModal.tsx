import { useEffect, useRef, useState } from 'react';
import { sessionStore, tryLogin } from '../../store/sessionStore';

// 登录页
export function LoginModal() {
  const open = sessionStore.use(s => s.appPhase === "login");
  const initialHint = sessionStore.use(s => s.loginHint);
  const savedKey = sessionStore.use(s => s.cfg?.taskKey);
  const loggingIn = sessionStore.use(s => s.loggingIn);

  const [key, setKey] = useState("");
  const [hint, setHint] = useState<{ text: string | null; bad: boolean }>({ text: null, bad: false });
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!open) return;
    setKey(savedKey || "");
    setHint({ text: initialHint || null, bad: !!initialHint });
    inputRef.current?.focus();
    inputRef.current?.select();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const submit = async () => {
    const k = key.trim();
    if (!k) { setHint({ text: "请输入 key", bad: true }); return; }
    setHint({ text: "验证中…", bad: false });
    const res = await tryLogin(k);
    if (!res.ok) setHint({ text: res.message ?? null, bad: true });
  };

  return (
    <div id="loginmask" className={"mask gate" + (open ? " on" : "")}>
      <div className="modal login">
        <h2>NONOKA<span className="grad">字幕</span> · 桌面版</h2>
        <p className="lead">输入 key 以连接云端</p>
        <div className="field">
          <input id="l-key" ref={inputRef} type="text" placeholder="向管理员索取" value={key}
            onChange={e => setKey(e.target.value)}
            onKeyDown={e => { if (e.key === "Enter") submit(); }} />
          <div id="l-hint" className={"hint" + (hint.bad ? " bad" : "")}>{hint.text || " "}</div>
        </div>
        <div className="modal-foot">
          <button id="l-go" className="btn primary" disabled={loggingIn} onClick={submit}>验证并进入</button>
        </div>
      </div>
    </div>
  );
}
