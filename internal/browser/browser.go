package browser

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
)

// Browser provides web automation capabilities via chromedp.
type Browser struct {
	allocCtx    context.Context
	allocCancel context.CancelFunc
	timeout     time.Duration
}

// NewBrowser creates a new Browser instance.
func NewBrowser() *Browser {
	return &Browser{
		timeout: 30 * time.Second,
	}
}

// Start initializes the browser.
func (b *Browser) Start(ctx context.Context) error {
	// Create allocator context (manages browser processes)
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WindowSize(1280, 720),
	)

	b.allocCtx, b.allocCancel = chromedp.NewExecAllocator(ctx, opts...)
	return nil
}

// Close shuts down the browser.
func (b *Browser) Close() error {
	if b.allocCancel != nil {
		b.allocCancel()
	}
	return nil
}

// NavigateResult is the result of navigating to a URL.
type NavigateResult struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Status  int    `json:"status"`
}

// Navigate navigates to a URL and returns page info.
func (b *Browser) Navigate(ctx context.Context, url string) (*NavigateResult, error) {
	result := &NavigateResult{URL: url}

	_, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()

	err := chromedp.Run(b.allocCtx,
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
		chromedp.Title(&result.Title),
	)
	if err != nil {
		return nil, fmt.Errorf("navigate to %s: %w", url, err)
	}

	return result, nil
}

// ScreenshotResult is the result of taking a screenshot.
type ScreenshotResult struct {
	URL   string `json:"url"`
	Data  []byte `json:"data"`   // PNG image data
	Width  int   `json:"width"`
	Height int   `json:"height"`
}

// Screenshot takes a screenshot of the current page.
func (b *Browser) Screenshot(ctx context.Context) (*ScreenshotResult, error) {
	result := &ScreenshotResult{}

	taskCtx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()

	var buf []byte
	err := chromedp.Run(taskCtx,
		chromedp.FullScreenshot(&buf, 100),
	)
	if err != nil {
		return nil, fmt.Errorf("screenshot: %w", err)
	}

	result.Data = buf
	return result, nil
}

// ClickResult is the result of clicking an element.
type ClickResult struct {
	Selector string `json:"selector"`
	Success  bool   `json:"success"`
}

// Click clicks an element by selector.
func (b *Browser) Click(ctx context.Context, selector string) (*ClickResult, error) {
	result := &ClickResult{Selector: selector}

	taskCtx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()

	err := chromedp.Run(taskCtx,
		chromedp.Click(selector),
	)
	if err != nil {
		return nil, fmt.Errorf("click %s: %w", selector, err)
	}

	result.Success = true
	return result, nil
}

// TypeResult is the result of typing text into an element.
type TypeResult struct {
	Selector string `json:"selector"`
	Text     string `json:"text"`
	Success  bool   `json:"success"`
}

// Type types text into an element by selector.
func (b *Browser) Type(ctx context.Context, selector, text string) (*TypeResult, error) {
	result := &TypeResult{Selector: selector, Text: text}

	taskCtx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()

	err := chromedp.Run(taskCtx,
		chromedp.WaitVisible(selector),
		chromedp.SendKeys(selector, text),
	)
	if err != nil {
		return nil, fmt.Errorf("type into %s: %w", selector, err)
	}

	result.Success = true
	return result, nil
}

// ExtractResult is the result of extracting text from the page.
type ExtractResult struct {
	Selector string `json:"selector"`
	Text     string `json:"text"`
}

// ExtractText extracts text content from an element.
func (b *Browser) ExtractText(ctx context.Context, selector string) (*ExtractResult, error) {
	result := &ExtractResult{Selector: selector}

	taskCtx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()

	err := chromedp.Run(taskCtx,
		chromedp.Text(selector, &result.Text),
	)
	if err != nil {
		return nil, fmt.Errorf("extract text from %s: %w", selector, err)
	}

	return result, nil
}

// EvaluateJSResult is the result of evaluating JavaScript.
type EvaluateJSResult struct {
	Expression string `json:"expression"`
	Result     string `json:"result"`
}

// EvaluateJS evaluates a JavaScript expression on the page.
func (b *Browser) EvaluateJS(ctx context.Context, expression string) (*EvaluateJSResult, error) {
	result := &EvaluateJSResult{Expression: expression}

	taskCtx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()

	err := chromedp.Run(taskCtx,
		chromedp.Evaluate(expression, &result.Result),
	)
	if err != nil {
		return nil, fmt.Errorf("evaluate JS: %w", err)
	}

	return result, nil
}
