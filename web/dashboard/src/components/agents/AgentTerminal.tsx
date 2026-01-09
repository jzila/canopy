import React, { useEffect, useRef } from 'react';
import { Terminal } from 'xterm';
import { FitAddon } from 'xterm-addon-fit';
import { useStateStore } from '../../stores/stateStore';
import 'xterm/css/xterm.css';

interface AgentTerminalProps {
  agentId: string;
}

export const AgentTerminal: React.FC<AgentTerminalProps> = ({ agentId }) => {
  const terminalRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const lastOutputRef = useRef<string>('');

  const agent = useStateStore((state) => state.agents[agentId]);

  // Initialize terminal on mount
  useEffect(() => {
    if (!terminalRef.current) return;

    const terminal = new Terminal({
      cursorBlink: false,
      disableStdin: true,
      fontSize: 13,
      fontFamily: 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Monaco, Consolas, monospace',
      theme: {
        background: '#1e1e1e',
        foreground: '#d4d4d4',
        cursor: '#d4d4d4',
        black: '#000000',
        brightBlack: '#666666',
        red: '#cd3131',
        brightRed: '#f14c4c',
        green: '#0dbc79',
        brightGreen: '#23d18b',
        yellow: '#e5e510',
        brightYellow: '#f5f543',
        blue: '#2472c8',
        brightBlue: '#3b8eea',
        magenta: '#bc3fbc',
        brightMagenta: '#d670d6',
        cyan: '#11a8cd',
        brightCyan: '#29b8db',
        white: '#e5e5e5',
        brightWhite: '#e5e5e5',
      },
      scrollback: 10000,
      convertEol: true,
    });

    const fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);

    terminal.open(terminalRef.current);
    fitAddon.fit();

    xtermRef.current = terminal;
    fitAddonRef.current = fitAddon;

    return () => {
      terminal.dispose();
      xtermRef.current = null;
      fitAddonRef.current = null;
      lastOutputRef.current = '';
    };
  }, []);

  // Handle terminal resize
  useEffect(() => {
    const handleResize = () => {
      if (fitAddonRef.current) {
        fitAddonRef.current.fit();
      }
    };

    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, []);

  // Update terminal output when agent output changes
  useEffect(() => {
    if (!xtermRef.current || !agent) return;

    const currentOutput = agent.output.stdout + agent.output.stderr;

    // Only write new content (diff from last known output)
    if (currentOutput !== lastOutputRef.current) {
      const newContent = currentOutput.slice(lastOutputRef.current.length);
      if (newContent) {
        xtermRef.current.write(newContent);
      }
      lastOutputRef.current = currentOutput;
    }
  }, [agent?.output.stdout, agent?.output.stderr, agent]);

  return (
    <div
      ref={terminalRef}
      style={{
        width: '100%',
        height: '100%',
        backgroundColor: '#1e1e1e',
      }}
    />
  );
};
