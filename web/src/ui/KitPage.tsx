import { useState } from 'react'
import { Badge, Button, Chip, Dialog, EmptyState, IdChip, Kbd, Popover, Skeleton, Tabs, ToastProvider, useToast, type ButtonVariant } from './index'

const VARIANTS: ButtonVariant[] = ['primary', 'secondary', 'ghost', 'danger', 'attention']
const SURFACES = ['--field', '--surface', '--raised', '--sunk', '--lichen', '--lantern', '--broken', '--ok']

/** Whether `#/kit` should be served: dev builds only. */
export function kitEnabled(path: string): boolean {
  return import.meta.env.DEV && path === '/kit'
}

/** The dev-only gallery at `#/kit`: every kit component in its states, on the live theme. */
export function KitPage() {
  return (
    <ToastProvider>
      <Kit />
    </ToastProvider>
  )
}

function Kit() {
  const toast = useToast()
  const [dialog, setDialog] = useState(false)
  const [tab, setTab] = useState('one')
  const [on, setOn] = useState(true)
  return (
    <div className="ui-kit">
      <h1>Kit</h1>
      <section>
        <h2>Tokens</h2>
        <div className="ui-kit-swatches">
          {SURFACES.map((t) => (
            <div key={t} className="ui-kit-swatch" style={{ background: `var(${t})` }}>
              <span style={{ background: 'var(--raised)', padding: '0 var(--s-1)' }}>{t}</span>
            </div>
          ))}
        </div>
      </section>
      <section>
        <h2>Button</h2>
        <div className="ui-kit-row">
          {VARIANTS.map((v) => (
            <Button key={v} variant={v}>
              {v}
            </Button>
          ))}
          <Button size="small">small</Button>
          <Button disabled>disabled</Button>
        </div>
      </section>
      <section>
        <h2>Badge, Chip, Kbd</h2>
        <div className="ui-kit-row">
          <Badge count={3} />
          <Badge count={4} tone="attention" label="waiting on you" />
          <Badge count={120} tone="bad" />
          <Chip kind="filter" selected={on} onClick={() => setOn(!on)}>
            Filter
          </Chip>
          <Chip kind="file">web/src/ui/Button.tsx</Chip>
          <Chip onRemove={() => {}}>topic</Chip>
          <Kbd keys="⌘K" />
          <Kbd keys={['g', 'i']} />
        </div>
      </section>
      <section>
        <h2>IdChip</h2>
        <div className="ui-kit-row">
          <IdChip kind="decision" id={275} />
          <IdChip kind="question" id={43} />
          <IdChip kind="run" id={311} bare />
          <IdChip kind="item" id={82} href="#/inbox" />
        </div>
      </section>
      <section>
        <h2>Tabs</h2>
        <Tabs
          label="Example"
          value={tab}
          onChange={setTab}
          items={[
            { id: 'one', label: 'Conversation', content: 'First panel' },
            { id: 'two', label: 'Changes', content: 'Second panel' },
          ]}
        />
      </section>
      <section>
        <h2>Dialog, Popover, Toast</h2>
        <div className="ui-kit-row">
          <Button onClick={() => setDialog(true)}>Open dialog</Button>
          <Popover label="Popover">Floats above the page.</Popover>
          <Button onClick={() => toast('Answer sent', 'ok')}>Toast</Button>
          <Button onClick={() => toast('Could not reach the daemon', 'bad')}>Error toast</Button>
        </div>
        <Dialog
          open={dialog}
          onClose={() => setDialog(false)}
          title="Close this lane?"
          actions={
            <>
              <Button variant="ghost" onClick={() => setDialog(false)}>
                Cancel
              </Button>
              <Button variant="danger" onClick={() => setDialog(false)}>
                Close lane
              </Button>
            </>
          }
        >
          Closing keeps the branch.
        </Dialog>
      </section>
      <section>
        <h2>EmptyState, Skeleton</h2>
        <EmptyState title="Nothing waits on you" action={<Button size="small">Open the board</Button>}>
          New decisions show up here.
        </EmptyState>
        <Skeleton lines={3} />
      </section>
    </div>
  )
}
