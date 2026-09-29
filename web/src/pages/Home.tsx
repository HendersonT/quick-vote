import { useState, type FormEvent } from "react";
import { createVote } from "../api";
import { navigate } from "../App";
import SettingsFields, {
  DEFAULT_SETTINGS_FORM,
  toSettings,
  type SettingsFormState,
} from "../components/SettingsFields";
import { addToHistory, getHistory, saveSession, sortHistory } from "../session";

/** Relative-ish date for the recent-votes list: "today", "3d ago", or a date. */
function formatHistoryDate(ts: number): string {
  const days = Math.floor((Date.now() - ts) / 86_400_000);
  if (days <= 0) return "today";
  if (days === 1) return "1d ago";
  if (days < 30) return `${days}d ago`;
  return new Date(ts).toLocaleDateString();
}

export default function Home() {
  const [title, setTitle] = useState("");
  const [creatorName, setCreatorName] = useState("");

  const [settings, setSettings] = useState<SettingsFormState>(DEFAULT_SETTINGS_FORM);

  const [fieldError, setFieldError] = useState<string | null>(null);
  const [serverError, setServerError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  // Open votes first, closed ones labeled and listed after (spec B3).
  const history = sortHistory(getHistory());

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setServerError(null);

    const trimmedTitle = title.trim();
    const trimmedName = creatorName.trim();

    if (!trimmedTitle) {
      setFieldError("Give the vote a title.");
      return;
    }
    if (trimmedTitle.length > 200) {
      setFieldError("Title must be 200 characters or fewer.");
      return;
    }
    if (!trimmedName) {
      setFieldError("Enter your name.");
      return;
    }
    if (trimmedName.length > 50) {
      setFieldError("Name must be 50 characters or fewer.");
      return;
    }
    setFieldError(null);

    setSubmitting(true);
    try {
      const { slug, creatorToken, sessionToken } = await createVote(
        trimmedTitle,
        trimmedName,
        toSettings(settings),
      );
      saveSession(slug, { sessionToken, creatorToken, name: trimmedName });
      addToHistory(slug, trimmedTitle);
      navigate(`/v/${slug}`);
    } catch (err) {
      setServerError(err instanceof Error ? err.message : "Failed to create vote.");
      setSubmitting(false);
    }
  }

  return (
    <main className="home-page">
      <h1>Quick Vote</h1>
      <p className="home-tagline">
        Suggest options, vote quadratically, land on a winner.
      </p>

      <form className="home-form" onSubmit={handleSubmit}>
        {(fieldError || serverError) && (
          <p className="error-banner" role="alert">
            {fieldError ?? serverError}
          </p>
        )}

        <div className="field">
          <label htmlFor="title">Vote title</label>
          <input
            id="title"
            type="text"
            value={title}
            maxLength={200}
            placeholder="Friday game night"
            onChange={(e) => setTitle(e.target.value)}
            required
          />
        </div>

        <div className="field">
          <label htmlFor="creatorName">Your name</label>
          <input
            id="creatorName"
            type="text"
            value={creatorName}
            maxLength={50}
            placeholder="Sam"
            onChange={(e) => setCreatorName(e.target.value)}
            required
          />
        </div>

        <SettingsFields value={settings} onChange={setSettings} />

        <div className="actions">
          <button type="submit" disabled={submitting}>
            {submitting ? "Creating…" : "Create vote"}
          </button>
        </div>
      </form>

      {history.length > 0 && (
        <section className="recent-votes">
          <h2>Recent votes</h2>
          <ul className="recent-votes-list">
            {history.map((entry) => (
              <li key={entry.slug} className="recent-votes-item">
                <a
                  href={`/v/${entry.slug}`}
                  onClick={(e) => {
                    e.preventDefault();
                    navigate(`/v/${entry.slug}`);
                  }}
                >
                  {entry.title}
                </a>
                <span className="recent-votes-meta">
                  {entry.closed && <span className="history-closed">closed</span>}
                  <span className="recent-votes-date">{formatHistoryDate(entry.ts)}</span>
                </span>
              </li>
            ))}
          </ul>
        </section>
      )}
    </main>
  );
}
