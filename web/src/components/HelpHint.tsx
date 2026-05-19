// HelpHint is a tiny inline "?" badge that surfaces a definition on
// hover. Used for non-obvious metric vocabulary ("what is a ship?")
// where the term would otherwise force the user to leave the
// dashboard to find out.
//
// Implementation is a styled <span> with title= so screen readers see
// the same definition the mouse-over does. Not a button — there's no
// click interaction; clicking would be a noop.

export function HelpHint({ text }: { text: string }) {
  return (
    <span className="help-hint" title={text} role="note" aria-label={text}>
      ?
    </span>
  );
}
