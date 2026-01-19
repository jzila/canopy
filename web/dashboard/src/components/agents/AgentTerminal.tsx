import React, { useEffect, useRef, useState } from 'react';
import { Terminal } from 'xterm';
import type { ITheme } from 'xterm';
import { FitAddon } from 'xterm-addon-fit';
import { useStateStore } from '../../stores/stateStore';
import 'xterm/css/xterm.css';

interface AgentTerminalProps {
  agentId: string;
}

// Dark theme for terminal
const darkTheme: ITheme = {
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
};

// Light theme for terminal
const lightTheme: ITheme = {
  background: '#f8f9fa',
  foreground: '#1e1e1e',
  cursor: '#1e1e1e',
  black: '#1e1e1e',
  brightBlack: '#666666',
  red: '#c41a16',
  brightRed: '#d62b2b',
  green: '#067d17',
  brightGreen: '#1a921a',
  yellow: '#9c7700',
  brightYellow: '#b5891d',
  blue: '#0451a5',
  brightBlue: '#0066cc',
  magenta: '#a626a4',
  brightMagenta: '#c839c8',
  cyan: '#0997b3',
  brightCyan: '#14a8cd',
  white: '#767676',
  brightWhite: '#1e1e1e',
};

// Hook to detect dark mode
const useDarkMode = () => {
  const [isDark, setIsDark] = useState(() =>
    document.documentElement.classList.contains('dark')
  );

  useEffect(() => {
    const observer = new MutationObserver(() => {
      setIsDark(document.documentElement.classList.contains('dark'));
    });

    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['class'],
    });

    return () => observer.disconnect();
  }, []);

  return isDark;
};

export const AgentTerminal: React.FC<AgentTerminalProps> = ({ agentId }) => {
  const terminalRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const lastOutputRef = useRef<string>('');

  const agent = useStateStore((state) => state.agents[agentId]);
  const isDark = useDarkMode();

  // Initialize terminal on mount
  useEffect(() => {
    if (!terminalRef.current) return;

    const terminal = new Terminal({
      cursorBlink: false,
      disableStdin: true,
      fontSize: 13,
      fontFamily: '"IBM Plex Mono", ui-monospace, SFMono-Regular, "SF Mono", Menlo, Monaco, Consolas, monospace',
      theme: isDark ? darkTheme : lightTheme,
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
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Update terminal theme when dark mode changes
  useEffect(() => {
    if (xtermRef.current) {
      xtermRef.current.options.theme = isDark ? darkTheme : lightTheme;
    }
  }, [isDark]);

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
      className="w-full h-full bg-[#f8f9fa] dark:bg-[#1e1e1e]"
    />
  );
};
