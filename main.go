package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
)

type worker struct {
	work      <-chan string
	ctx       context.Context
	logger    *log.Logger
	errLogger *log.Logger
	done      chan<- struct{}
}

func (w *worker) listen() {
	for {
		select {
		case work := <-w.work:
			err := w.convertFile(work)
			if err != nil {
				w.errLogger.Printf("Error converting %s: %v", work, err)
			}
		case <-w.ctx.Done():
			w.done <- struct{}{}
			return
		}
	}
}

func (w *worker) convertFile(filename string) error {
	newFileName := strings.Replace(filename, ".mkv", ".mp4", 1)
	w.logger.Printf("Converting %s to %s\n", filename, newFileName)
	cmd := exec.Command("ffmpeg", "-i", filename, "-codec", "copy", newFileName)
	if err := cmd.Run(); err != nil {
		return err
	}

	w.logger.Printf("Removing %s\n", filename)
	return os.Remove(filename)
}

func main() {
	dir := flag.String("d", "", "directory to search")
	file := flag.String("f", "", "file to convert")
	recurse := flag.Bool("r", false, "search directory recursively")
	verbose := flag.Bool("v", false, "verbose")
	workers := flag.Int("c", 1, "number of concurrent conversions")
	logFileLoc := flag.String("l", "", "location for file logging")

	flag.Parse()

	if *dir == "" && *file == "" {
		log.Fatal("no input supplied")
	} else if *dir != "" && *file != "" {
		log.Fatal("too many inputs supplied")
	} else if *workers < 1 {
		*workers = 1
	}

	// setup info logger
	var (
		logOut  io.Writer
		logFile *os.File
		err     error
	)
	if *logFileLoc != "" {
		logFile, err = os.OpenFile(*logFileLoc, os.O_RDWR|os.O_CREATE, os.ModeAppend)
		if err != nil {
			log.Fatal(err)
		}
		defer logFile.Close()
		logOut = logFile
	}

	if *verbose {
		if logOut == nil {
			logOut = os.Stdout
		} else {
			logOut = io.MultiWriter(logOut, os.Stdout)
		}
	}
	if logOut == nil {
		logOut = ioutil.Discard
	}
	logger := log.New(logOut, "", log.LstdFlags)

	// setup error logger
	var logOutErr io.Writer = os.Stderr
	if logFile != nil {
		logOutErr = io.MultiWriter(logOut, logFile)
	}
	errLogger := log.New(logOutErr, "", log.LstdFlags)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	defer func() {
		// cancel and wait for response from all workers
		cancel()
		for i := 0; i < *workers; i++ {
			<-done
		}
	}()

	work := make(chan string)
	for i := 0; i < *workers; i++ {
		w := &worker{work: work, ctx: ctx, logger: logger, errLogger: errLogger, done: done}
		go w.listen()
	}

	if *dir != "" {
		err = convertDirectory(*dir, *recurse, work)
		if err != nil {
			errLogger.Fatal(err)
		}
	} else {
		if !strings.HasSuffix(*file, ".mkv") {
			err = fmt.Errorf("%s not a mkv file", *file)
		} else {
			work <- *file
		}
	}
	if err != nil {
		errLogger.Fatal(err)
	}
}

func convertDirectory(dirname string, recurse bool, convert chan<- string) error {
	if info, err := os.Stat(dirname); err != nil {
		return err
	} else if !info.IsDir() {
		return fmt.Errorf("%s not a directory", dirname)
	}

	if !strings.HasSuffix(dirname, "/") {
		dirname += "/"
	}

	files, err := ioutil.ReadDir(dirname)
	if err != nil {
		return err
	}

	for _, f := range files {
		if f.IsDir() {
			if recurse {
				convertDirectory(dirname+f.Name(), recurse, convert)
			}
			continue
		}

		if strings.HasSuffix(f.Name(), ".mkv") {
			convert <- dirname + f.Name()
		}
	}
	return nil
}
