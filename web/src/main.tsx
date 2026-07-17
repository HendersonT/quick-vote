import React from "react";
import ReactDOM from "react-dom/client";
import "./styles.css";

// App.tsx (router, pages) lands in a later task; this placeholder keeps the
// scaffold buildable and runnable on its own.
function Placeholder() {
  return <div>Quick Vote</div>;
}

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <Placeholder />
  </React.StrictMode>,
);
