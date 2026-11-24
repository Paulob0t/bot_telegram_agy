import 'package:flutter/material.dart';

import '../models/agent_event.dart';

/// Chat message bubble supporting multi-message selection mode and clean
/// zero-token Markdown formatting.
class ChatMessageBubble extends StatelessWidget {
  final ChatMessage message;
  final bool selectionMode;
  final bool isSelected;
  final VoidCallback? onToggleSelect;
  final VoidCallback? onCopy;
  final VoidCallback? onDelete;

  const ChatMessageBubble({
    super.key,
    required this.message,
    this.selectionMode = false,
    this.isSelected = false,
    this.onToggleSelect,
    this.onCopy,
    this.onDelete,
  });

  @override
  Widget build(BuildContext context) {
    final bgColor = isSelected
        ? const Color(0xFF28221E)
        : (message.isUser
            ? const Color(0xFF21262D)
            : (message.isError ? const Color(0xFF2D1619) : const Color(0xFF161B22)));
    final borderColor = isSelected
        ? const Color(0xFFD97757)
        : (message.isUser
            ? const Color(0xFF30363D)
            : (message.isError ? const Color(0xFFF85149) : const Color(0xFF21262D)));

    final bubble = Container(
      margin: const EdgeInsets.symmetric(vertical: 5),
      padding: const EdgeInsets.symmetric(horizontal: 13, vertical: 10),
      constraints: const BoxConstraints(maxWidth: 330),
      decoration: BoxDecoration(
        color: bgColor,
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: borderColor, width: isSelected ? 1.5 : 1.0),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (message.text.isNotEmpty)
            FormattedMessageText(
              rawText: message.text,
              selectionMode: selectionMode,
              onTap: selectionMode ? onToggleSelect : null,
            ),
          if (message.toolStatus.isNotEmpty) ...[
            if (message.text.isNotEmpty) const SizedBox(height: 6),
            Text(
              message.toolStatus,
              style: const TextStyle(
                color: Color(0xFFD29922),
                fontSize: 12,
                fontFamily: 'monospace',
              ),
            ),
          ],
        ],
      ),
    );

    return GestureDetector(
      behavior: HitTestBehavior.opaque,
      onTap: selectionMode ? onToggleSelect : null,
      onLongPress: onToggleSelect,
      child: Row(
        mainAxisAlignment:
            message.isUser ? MainAxisAlignment.end : MainAxisAlignment.start,
        crossAxisAlignment: CrossAxisAlignment.center,
        children: [
          if (selectionMode) ...[
            Padding(
              padding: const EdgeInsets.only(right: 10),
              child: Icon(
                isSelected
                    ? Icons.check_circle_rounded
                    : Icons.radio_button_unchecked_rounded,
                size: 20,
                color: isSelected
                    ? const Color(0xFFD97757)
                    : const Color(0xFF6E7681),
              ),
            ),
          ],
          Flexible(child: bubble),
        ],
      ),
    );
  }
}

/// Zero-token client-side Markdown & noise cleaner for chat messages:
/// - Strips `root agent idle; waiting for ... background task(s) to finish`
/// - Converts `[label](file:///long/path)` into clean inline code `label`
/// - Renders headings, bullet lists, **bold**, *italic* and `inline code` cleanly.
class FormattedMessageText extends StatelessWidget {
  final String rawText;
  final bool selectionMode;
  final VoidCallback? onTap;

  const FormattedMessageText({
    super.key,
    required this.rawText,
    required this.selectionMode,
    this.onTap,
  });

  static String cleanDisplay(String input) {
    var out = input.replaceAll(
      RegExp(
        r'^root agent idle; waiting for \d+ background task\(s\) to finish\s*$',
        multiLine: true,
      ),
      '',
    );
    out = out.replaceAllMapped(
      RegExp(r'\[([^\]]+)\]\((?:file|https?|conversation)://[^\)]+\)'),
      (m) => '`${m.group(1)}`',
    );
    return out.trim();
  }

  List<InlineSpan> _parseInline(String text) {
    final spans = <InlineSpan>[];
    final pattern = RegExp(r'(`[^`]+`|\*\*[^*]+\*\*|\*[^*\n]+\*)');
    int last = 0;
    for (final m in pattern.allMatches(text)) {
      if (m.start > last) {
        spans.add(TextSpan(text: text.substring(last, m.start)));
      }
      final tok = m.group(0)!;
      if (tok.startsWith('`') && tok.endsWith('`') && tok.length >= 2) {
        spans.add(
          TextSpan(
            text: tok.substring(1, tok.length - 1),
            style: const TextStyle(
              color: Color(0xFF79C0FF),
              backgroundColor: Color(0xFF1F242C),
              fontFamily: 'monospace',
              fontSize: 12.5,
            ),
          ),
        );
      } else if (tok.startsWith('**') && tok.endsWith('**') && tok.length >= 4) {
        spans.add(
          TextSpan(
            text: tok.substring(2, tok.length - 2),
            style: const TextStyle(
              color: Colors.white,
              fontWeight: FontWeight.w600,
            ),
          ),
        );
      } else if (tok.startsWith('*') && tok.endsWith('*') && tok.length >= 2) {
        spans.add(
          TextSpan(
            text: tok.substring(1, tok.length - 1),
            style: const TextStyle(
              color: Color(0xFFE6EDF3),
              fontWeight: FontWeight.w600,
            ),
          ),
        );
      }
      last = m.end;
    }
    if (last < text.length) {
      spans.add(TextSpan(text: text.substring(last)));
    }
    return spans;
  }

  @override
  Widget build(BuildContext context) {
    final cleaned = cleanDisplay(rawText);
    if (cleaned.isEmpty) return const SizedBox.shrink();

    final lines = cleaned.split('\n');
    final spans = <InlineSpan>[];

    for (int i = 0; i < lines.length; i++) {
      var line = lines[i];
      if (line.startsWith('— ')) {
        spans.add(
          TextSpan(
            text: line,
            style: const TextStyle(
              color: Color(0xFF6E7681),
              fontSize: 11.5,
              fontFamily: 'monospace',
            ),
          ),
        );
      } else if (line.startsWith('### ') ||
          line.startsWith('## ') ||
          line.startsWith('# ')) {
        final heading = line.replaceFirst(RegExp(r'^#{1,3}\s+'), '');
        spans.add(
          TextSpan(
            text: heading,
            style: const TextStyle(
              color: Colors.white,
              fontSize: 14,
              fontWeight: FontWeight.w700,
            ),
          ),
        );
      } else {
        if (line.startsWith('- ')) {
          line = '• ${line.substring(2)}';
        } else if (line.startsWith('  - ')) {
          line = '  • ${line.substring(4)}';
        }
        spans.addAll(_parseInline(line));
      }
      if (i < lines.length - 1) {
        spans.add(const TextSpan(text: '\n'));
      }
    }

    const baseStyle = TextStyle(
      color: Color(0xFFE6EDF3),
      fontSize: 13.5,
      height: 1.42,
    );

    if (selectionMode) {
      return RichText(
        text: TextSpan(style: baseStyle, children: spans),
      );
    }
    return SelectableText.rich(
      TextSpan(style: baseStyle, children: spans),
      onTap: onTap,
    );
  }
}
