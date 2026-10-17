/**
 * Whether a dialog is on screen: open or displayed, not one that is only in the DOM (the app
 * keeps some mounted, hidden). Any Esc handler on a page asks this before it closes the page.
 */
export function dialogShown(): boolean {
  return [...document.querySelectorAll<HTMLElement>('dialog[open], [role="dialog"], [role="alertdialog"], [aria-modal="true"]')].some((el) => {
    if (el.closest('[hidden], [aria-hidden="true"]')) return false
    const style = window.getComputedStyle(el)
    return style.display !== 'none' && style.visibility !== 'hidden'
  })
}

/** Whether a key goes to a field the person is typing in: Esc there belongs to the field. */
export function isEditable(target: EventTarget | null): boolean {
  return target instanceof HTMLElement && !!target.closest('input, textarea, select, [contenteditable]')
}
